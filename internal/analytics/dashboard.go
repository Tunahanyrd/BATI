package analytics

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bati/internal/db"
	"bati/internal/dto"
	"bati/internal/model"
	"bati/internal/sysfs"
)

// FetchDashboardData queries and aggregates every value consumed by the GUI.
func FetchDashboardData(database *db.DB, now time.Time, use24h bool) (*dto.DashboardDTO, error) {
	now = now.UTC()

	latest, err := database.GetLastTelemetryBefore(now)
	var latestSampleTime time.Time
	isStale := false
	if err == nil {
		latestSampleTime = latest.Timestamp
		if now.Sub(latestSampleTime) >= 10*time.Minute {
			isStale = true
		}
	}

	// Anchor time boundaries ending at latestSampleTime if stale, to prevent screen active duration from countdown bug
	var end time.Time
	if !latestSampleTime.IsZero() && isStale {
		end = latestSampleTime
	} else {
		end = now
	}

	start := end.Add(-24 * time.Hour)
	if !use24h {
		localEnd := end.In(time.Local)
		start = time.Date(
			localEnd.Year(),
			localEnd.Month(),
			localEnd.Day(),
			0, 0, 0, 0,
			localEnd.Location(),
		).AddDate(0, 0, -9).UTC()
	}

	result := &dto.DashboardDTO{}

	switch {
	case err == nil:
		result.Status = statusDTO(latest)
	case errors.Is(err, sql.ErrNoRows):
		// A newly-created database is a valid empty state.
	default:
		return nil, fmt.Errorf("fetch latest telemetry: %w", err)
	}

	device, err := database.GetPrimaryDevice()
	switch {
	case err == nil:
		result.Health = healthDTO(device)
	case errors.Is(err, sql.ErrNoRows):
		// If no primary device exists in DB, we can still have an empty health object
		result.Health.Available = false
	default:
		return nil, fmt.Errorf("fetch primary device: %w", err)
	}

	liveSnapshot, liveDevice, liveErr := fetchLiveSnapshot(device.ID)
	if liveErr == nil {
		if !result.Health.Available {
			result.Health = healthDTO(liveDevice)
		}
		result.Health.ChargeLimitPercent = liveDevice.ChargeLimitPercent
		result.Health.ChargeLimitAvailable = liveDevice.ChargeLimitAvailable
		result.Health.ChargeStartThresholdPercent = liveDevice.ChargeStartThresholdPercent
	}

	// 1. Historical Snapshot DTO
	if err == nil {
		result.HistoricalSnapshot = dto.HistoricalSnapshotDTO{
			Available:       true,
			Timestamp:       latest.Timestamp,
			Age:             now.Sub(latest.Timestamp),
			Stale:           isStale,
			CapacityPercent: latest.Capacity,
			Status:          latest.Status,
			PowerRateW:      latest.EnergyRate,
			VoltageV:        latest.Voltage,
		}
	}

	// 2. Live Snapshot DTO (fetch dynamically from sysfs/upower if available)
	if liveErr == nil {
		result.LiveSnapshot = liveSnapshot
		// Override health cycle count with live value
		if liveDevice.CycleCount > 0 {
			result.Health.CycleCount = liveDevice.CycleCount
			result.Health.CycleCountAvailable = true
		}
	}

	// Try reading current live snapshot telemetry from sysfs (for backwards compatibility if needed)
	if result.LiveSnapshot.Available {
		result.Live = dto.LiveBatteryDTO{
			Available:  true,
			Timestamp:  result.LiveSnapshot.Timestamp,
			Capacity:   result.LiveSnapshot.CapacityPercent,
			Status:     result.LiveSnapshot.Status,
			EnergyRate: result.LiveSnapshot.PowerRateW,
			Voltage:    result.LiveSnapshot.VoltageV,
		}
	}

	if report, reportErr := CalculateOvernightDrainAt(database, end); reportErr == nil {
		result.Overnight = dto.OvernightDrainDTO{
			HasReport:  true,
			StartTime:  report.StartTime,
			EndTime:    report.EndTime,
			StartPct:   report.StartPct,
			EndPct:     report.EndPct,
			DrainPct:   report.Drain,
			Duration:   report.Duration,
			Type:       report.Type,
			Confidence: report.Provenance,
		}
	}

	// Retrieve telemetry relative to query range (start to end)
	telemetry, err := database.GetTelemetryRange(start, end)
	if err != nil {
		return nil, fmt.Errorf("fetch telemetry range: %w", err)
	}

	summary, err := GenerateRangeSummary(database, start, end)
	if err != nil {
		return nil, fmt.Errorf("generate range summary: %w", err)
	}

	result.Timeline = BuildTimelineDTO(telemetry, summary.Sessions, start, end, use24h)
	if lastCharge, chargeErr := database.GetLastFullChargeBefore(now); chargeErr == nil {
		result.LastCharge = dto.LastChargeDTO{
			Available: true,
			Timestamp: lastCharge.Timestamp,
			Capacity:  lastCharge.Capacity,
		}
	} else if !errors.Is(chargeErr, sql.ErrNoRows) {
		return nil, fmt.Errorf("fetch last full charge: %w", chargeErr)
	}
	result.TotalDischarge = summary.TotalDischarge
	result.TotalCharge = summary.TotalCharge
	result.ActiveDuration = summary.ActiveDuration
	result.SleepDuration = summary.SleepDuration
	result.Summary = buildRangeSummaryDTO(summary, result.Timeline)
	result.RecentSummary = result.Summary

	// If 10-day view, compute the last 24h summary anchored to recentEnd
	if !use24h {
		recentStart := end.Add(-24 * time.Hour)
		recentSummary, recentErr := GenerateRangeSummary(database, recentStart, end)
		if recentErr != nil {
			return nil, fmt.Errorf("generate recent summary: %w", recentErr)
		}
		recentTimeline := dto.TimelineDTO{}
		for _, point := range telemetry {
			if point.Timestamp.Before(recentStart) || point.Timestamp.After(end) {
				continue
			}
			if recentTimeline.AvailableFrom.IsZero() {
				recentTimeline.AvailableFrom = point.Timestamp
			}
			recentTimeline.AvailableTo = point.Timestamp
		}
		result.RecentSummary = buildRangeSummaryDTO(recentSummary, recentTimeline)
	}
	return result, nil
}

// FetchLiveSnapshot reads only the current sysfs battery state without touching SQLite history.
func FetchLiveSnapshot(preferredDeviceID string) (dto.LiveSnapshotDTO, error) {
	snapshot, _, err := fetchLiveSnapshot(preferredDeviceID)
	return snapshot, err
}

func fetchLiveSnapshot(preferredDeviceID string) (dto.LiveSnapshotDTO, model.Device, error) {
	sysDevices, err := sysfs.ReadBatteryDevices()
	if err != nil {
		return dto.LiveSnapshotDTO{}, model.Device{}, fmt.Errorf("read live battery devices: %w", err)
	}
	if len(sysDevices) == 0 {
		return dto.LiveSnapshotDTO{}, model.Device{}, errors.New("no live battery devices")
	}

	target := sysDevices[0]
	if preferredDeviceID != "" {
		for _, device := range sysDevices {
			if device.ID == preferredDeviceID {
				target = device
				break
			}
		}
	}

	liveTel, err := sysfs.ReadTelemetry(target.ID, false)
	if err != nil {
		return dto.LiveSnapshotDTO{}, model.Device{}, fmt.Errorf("read live telemetry: %w", err)
	}
	capLevel := sysfs.ReadCapacityLevel(target.ID)
	energyNow := sysfs.ReadEnergyNow(target.ID)

	dirPath := filepath.Join("/sys/class/power_supply", target.ID)
	_, errPower := os.Stat(filepath.Join(dirPath, "power_now"))
	_, errCurrent := os.Stat(filepath.Join(dirPath, "current_now"))
	powerAvailable := errPower == nil || errCurrent == nil

	_, errVoltage := os.Stat(filepath.Join(dirPath, "voltage_now"))
	voltageAvailable := errVoltage == nil

	return dto.LiveSnapshotDTO{
		Available:                     true,
		Source:                        "sysfs",
		Timestamp:                     liveTel.Timestamp,
		CapacityPercent:               liveTel.Capacity,
		CapacityLevel:                 capLevel,
		Status:                        liveTel.Status,
		PowerRateW:                    liveTel.EnergyRate,
		PowerRateAvailable:            powerAvailable,
		VoltageV:                      liveTel.Voltage,
		VoltageAvailable:              voltageAvailable,
		EnergyNowWh:                   energyNow,
		EnergyFullWh:                  target.FullCapacity,
		EnergyFullDesignWh:            target.DesignCapacity,
		CycleCount:                    target.CycleCount,
		Manufacturer:                  target.Vendor,
		ModelName:                     target.Model,
		Technology:                    target.Technology,
		ChargeLimitPercent:            target.ChargeLimitPercent,
		ChargeLimitAvailable:          target.ChargeLimitAvailable,
		ChargeStartThresholdPercent:   target.ChargeStartThresholdPercent,
		ChargeStartThresholdAvailable: target.ChargeLimitAvailable,
	}, target, nil
}

func statusDTO(t model.Telemetry) dto.BatteryStatusDTO {
	status := dto.BatteryStatusDTO{
		Available:  true,
		Timestamp:  t.Timestamp,
		Capacity:   t.Capacity,
		Status:     t.Status,
		EnergyRate: t.EnergyRate,
		Voltage:    t.Voltage,
		PluggedIn:  isPluggedIn(t.Status),
	}
	return status
}

func healthDTO(device model.Device) dto.DeviceHealthDTO {
	health := dto.DeviceHealthDTO{
		Available:           true,
		CycleCountAvailable: device.CycleCount > 0,
		Model:               strings.TrimSpace(device.Model),
		Vendor:              strings.TrimSpace(device.Vendor),
		DesignCapacity:      device.DesignCapacity,
		FullCapacity:        device.FullCapacity,
		CycleCount:          device.CycleCount,
		Technology:          strings.TrimSpace(device.Technology),
	}
	if device.DesignCapacity > 0 && device.FullCapacity > 0 {
		health.HealthAvailable = true
		health.HealthPct = min(100, device.FullCapacity/device.DesignCapacity*100)
	}
	return health
}

func isPluggedIn(status string) bool {
	switch normalizedStatus(status) {
	case "charging", "full", "not_charging":
		return true
	default:
		return false
	}
}

func normalizedStatus(status string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(status)), " ", "_")
}

// BuildTimelineDTO converts query results into render-ready points and screen-time bins.
func BuildTimelineDTO(
	telemetry []model.Telemetry,
	sessions []model.Session,
	start, end time.Time,
	use24h bool,
) dto.TimelineDTO {
	validTelemetry := make([]model.Telemetry, 0, len(telemetry))
	points := make([]dto.TimelinePointDTO, 0, len(telemetry))
	for _, point := range telemetry {
		if !point.ValidCapacity() {
			continue
		}
		validTelemetry = append(validTelemetry, point)
		points = append(points, dto.TimelinePointDTO{
			Timestamp:  point.Timestamp,
			Capacity:   point.Capacity,
			Status:     point.Status,
			ScreenOn:   point.ScreenOn,
			EnergyRate: point.EnergyRate,
			Voltage:    point.Voltage,
		})
	}

	bars := buildScreenBars(start, end, use24h)
	addScreenDurations(bars, validTelemetry, start, end)

	timeline := dto.TimelineDTO{
		Start:    start,
		End:      end,
		Points:   points,
		SOTBars:  bars,
		Sessions: append([]model.Session(nil), sessions...),
	}
	if len(points) > 0 {
		timeline.AvailableFrom = points[0].Timestamp
		timeline.AvailableTo = points[len(points)-1].Timestamp
	}
	if !use24h {
		timeline.Days = buildDailySummaries(bars, sessions)
	}
	return timeline
}

func buildScreenBars(start, end time.Time, use24h bool) []dto.SOTBarDTO {
	if !end.After(start) {
		return nil
	}
	if use24h {
		const binDuration = 30 * time.Minute
		bars := make([]dto.SOTBarDTO, 0, 48)
		for binStart := start; binStart.Before(end); binStart = binStart.Add(binDuration) {
			binEnd := minTime(binStart.Add(binDuration), end)
			bars = append(bars, dto.SOTBarDTO{
				Start: binStart.UTC(),
				End:   binEnd.UTC(),
				Label: binStart.Local().Format("15:04"),
			})
		}
		return bars
	}

	location := time.Local
	localStart := start.In(location)
	dayStart := time.Date(
		localStart.Year(),
		localStart.Month(),
		localStart.Day(),
		0, 0, 0, 0,
		location,
	)
	bars := make([]dto.SOTBarDTO, 0, 10)
	for dayStart.Before(end.In(location)) && len(bars) < 10 {
		nextDay := dayStart.AddDate(0, 0, 1)
		binStart := maxTime(dayStart.UTC(), start)
		binEnd := minTime(nextDay.UTC(), end)
		if binEnd.After(binStart) {
			bars = append(bars, dto.SOTBarDTO{
				Start: binStart.UTC(),
				End:   binEnd.UTC(),
				Label: dayStart.Format("02 Jan"),
			})
		}
		dayStart = nextDay
	}
	return bars
}

func addScreenDurations(
	bars []dto.SOTBarDTO,
	telemetry []model.Telemetry,
	rangeStart, rangeEnd time.Time,
) {
	const defaultInterval = 5 * time.Minute
	const maximumObservedGap = 10 * time.Minute

	for i, point := range telemetry {
		segmentEnd := point.Timestamp.Add(defaultInterval)
		if i+1 < len(telemetry) {
			gap := telemetry[i+1].Timestamp.Sub(point.Timestamp)
			if gap > 0 && gap <= maximumObservedGap {
				segmentEnd = telemetry[i+1].Timestamp
			}
		}
		if segmentEnd.After(rangeEnd) {
			segmentEnd = rangeEnd
		}
		segmentStart := maxTime(point.Timestamp, rangeStart)
		if !segmentEnd.After(segmentStart) {
			continue
		}
		for barIndex := range bars {
			overlapStart := maxTime(segmentStart, bars[barIndex].Start)
			overlapEnd := minTime(segmentEnd, bars[barIndex].End)
			if overlapEnd.After(overlapStart) {
				overlap := overlapEnd.Sub(overlapStart)
				bars[barIndex].Coverage += overlap
				if point.ScreenOn {
					bars[barIndex].Duration += overlap
				}
			}
		}
	}
	for index := range bars {
		bars[index].Observed = bars[index].Coverage > 0
		interval := bars[index].End.Sub(bars[index].Start)
		bars[index].Partial = interval <= 0 || bars[index].Coverage < interval*3/4
	}
}

func buildRangeSummaryDTO(summary *DailySummary, timeline dto.TimelineDTO) dto.RangeSummaryDTO {
	result := dto.RangeSummaryDTO{
		TotalDischarge: summary.TotalDischarge,
		TotalCharge:    summary.TotalCharge,
		ActiveDuration: summary.ActiveDuration,
		SleepDuration:  summary.SleepDuration,
		Provenance:     summary.Provenance,
	}
	if timeline.AvailableTo.After(timeline.AvailableFrom) {
		result.AvailableDuration = timeline.AvailableTo.Sub(timeline.AvailableFrom)
	}
	for _, session := range summary.Sessions {
		if normalizedStatus(session.Type) == "charging" {
			result.ChargingDuration += session.Duration
		}
		if session.Provenance == "estimated" {
			result.Provenance = "estimated"
		} else if session.Provenance == "inferred" && result.Provenance != "estimated" {
			result.Provenance = "inferred"
		}
	}
	return result
}

func buildDailySummaries(bars []dto.SOTBarDTO, sessions []model.Session) []dto.DailySummaryDTO {
	days := make([]dto.DailySummaryDTO, 0, len(bars))
	for _, bar := range bars {
		day := dto.DailySummaryDTO{
			Start:          bar.Start,
			End:            bar.End,
			Label:          bar.Label,
			ActiveDuration: bar.Duration,
			Coverage:       bar.Coverage,
			Observed:       bar.Observed,
			Partial:        bar.Partial,
			Provenance:     "observed",
		}
		for _, session := range sessions {
			overlapStart := maxTime(session.StartTime, bar.Start)
			overlapEnd := minTime(session.EndTime, bar.End)
			if !overlapEnd.After(overlapStart) {
				continue
			}
			overlap := overlapEnd.Sub(overlapStart)
			day.Observed = true
			if session.Provenance == "estimated" {
				day.Provenance = "estimated"
			} else if session.Provenance == "inferred" && day.Provenance != "estimated" {
				day.Provenance = "inferred"
			}
			switch normalizedStatus(session.Type) {
			case "charging":
				day.ChargingDuration += overlap
			case "sleeping":
				day.SleepDuration += overlap
			}
			if session.Duration <= 0 {
				continue
			}
			delta := session.DeltaPct * float64(overlap) / float64(session.Duration)
			switch normalizedStatus(session.Type) {
			case "charging":
				if delta > 0 {
					day.TotalCharge += delta
				}
			case "discharging", "sleeping":
				if delta < 0 {
					day.TotalDischarge += -delta
				} else if delta > 0 {
					day.TotalCharge += delta
				}
			}
		}
		days = append(days, day)
	}
	return days
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
