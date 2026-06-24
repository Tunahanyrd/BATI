package gui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"bati/internal/dto"
	"bati/internal/model"
	"bati/internal/statusfmt"
)

type tooltipModel struct {
	Title string
	Rows  []string
}

func placeTooltip(anchor image.Point, bounds image.Rectangle, size image.Point, margin int) image.Rectangle {
	if size.X > bounds.Dx() {
		size.X = bounds.Dx()
	}
	if size.Y > bounds.Dy() {
		size.Y = bounds.Dy()
	}
	candidates := []image.Point{
		{X: anchor.X + margin, Y: anchor.Y + margin},
		{X: anchor.X - size.X - margin, Y: anchor.Y + margin},
		{X: anchor.X + margin, Y: anchor.Y - size.Y - margin},
		{X: anchor.X - size.X - margin, Y: anchor.Y - size.Y - margin},
	}
	for _, candidate := range candidates {
		rectangle := image.Rectangle{Min: candidate, Max: candidate.Add(size)}
		if rectangle.In(bounds) {
			return rectangle
		}
	}
	left := min(max(anchor.X+margin, bounds.Min.X), bounds.Max.X-size.X)
	top := min(max(anchor.Y+margin, bounds.Min.Y), bounds.Max.Y-size.Y)
	return image.Rect(left, top, left+size.X, top+size.Y)
}

func pointTooltip(point dto.TimelinePointDTO, session *model.Session, hoverTime time.Time, location *time.Location) tooltipModel {
	screenState := "inactive"
	if point.ScreenOn {
		screenState = "active"
	}

	var sessionRows []string
	if session != nil {
		sType := normalizedSessionType(session.Type)
		sStart := session.StartTime
		sEnd := session.EndTime

		isStart := point.Timestamp.Sub(sStart) >= -30*time.Second && point.Timestamp.Sub(sStart) <= 30*time.Second
		isEnd := point.Timestamp.Sub(sEnd) >= -30*time.Second && point.Timestamp.Sub(sEnd) <= 30*time.Second

		var displayName string
		if sType == "sleeping" {
			if isStart {
				displayName = "sleep start"
			} else if isEnd {
				displayName = "resume"
			} else {
				displayName = "sleep"
			}
			// Force screen to be inactive if it's within the sleep window
			if !point.Timestamp.Before(sStart) && !point.Timestamp.After(sEnd) {
				screenState = "inactive"
			}
		} else if sType == "charging" {
			if isStart {
				displayName = "charge start"
			} else {
				displayName = "charging"
			}
		} else {
			displayName = displaySessionType(session.Type)
		}

		sessionRows = append(sessionRows, fmt.Sprintf(
			"session: %s for %s",
			strings.ToLower(displayName),
			formatDuration(session.Duration),
		))
	}

	rows := []string{
		fmt.Sprintf("battery: %.0f%%", point.Capacity),
		"state: " + statusfmt.Lower(point.Status),
		fmt.Sprintf("rate: %.2f W", point.EnergyRate),
		"screen: " + screenState,
	}
	if len(sessionRows) > 0 {
		rows = append(rows, sessionRows...)
	}

	return tooltipModel{
		Title: strings.ToLower("battery sample · " + hoverTime.In(location).Format("02 Jan, 15:04")),
		Rows:  rows,
	}
}

func sessionTooltip(session model.Session, hoverTime time.Time, location *time.Location) tooltipModel {
	title := strings.ToLower("battery state · " + hoverTime.In(location).Format("02 Jan, 15:04"))
	sessionTypeRow := strings.ToLower(displaySessionType(session.Type)) + " session"
	interval := fmt.Sprintf(
		"%s → %s",
		session.StartTime.In(location).Format("15:04"),
		session.EndTime.In(location).Format("15:04"),
	)
	provenance := normalizedProvenance(session.Provenance)
	rows := []string{sessionTypeRow, interval}
	switch normalizedSessionType(session.Type) {
	case "sleeping":
		rows = append(rows,
			formatDuration(session.Duration),
			fmt.Sprintf("battery: %.0f%% → %.0f%%", session.StartPct, session.EndPct),
			fmt.Sprintf("drain: %+.1f%%", session.DeltaPct),
		)
	case "charging":
		rows = append(rows,
			fmt.Sprintf("%.0f%% → %.0f%%", session.StartPct, session.EndPct),
			fmt.Sprintf("%+.1f%% over %s", session.DeltaPct, formatDuration(session.Duration)),
		)
	case "not_charging":
		rows = append(rows,
			fmt.Sprintf("battery held at %.0f%%", session.EndPct),
			"plugged in",
		)
	case "full":
		rows = append(rows,
			fmt.Sprintf("battery held at %.0f%%", session.EndPct),
			"plugged in",
		)
	default:
		rows = append(rows,
			fmt.Sprintf("%.0f%% → %.0f%%", session.StartPct, session.EndPct),
			fmt.Sprintf("%+.1f%% over %s", session.DeltaPct, formatDuration(session.Duration)),
		)
	}
	if provenance != "" {
		rows = append(rows, provenance)
	}
	return tooltipModel{Title: title, Rows: rows}
}

func chargeLimitTooltip(limit chargeLimitLineModel, live dto.LiveSnapshotDTO) tooltipModel {
	if !limit.Available {
		return tooltipModel{}
	}
	rows := []string{"configured end threshold"}
	if live.Available {
		rows = append(rows, fmt.Sprintf("current battery: %.0f%%", live.CapacityPercent))
	}
	return tooltipModel{
		Title: fmt.Sprintf("charge limit · %d%%", limit.Percent),
		Rows:  rows,
	}
}

func currentBatteryTooltip(marker currentBatteryMarkerModel) tooltipModel {
	if !marker.Available {
		return tooltipModel{}
	}
	rows := []string{
		fmt.Sprintf("battery: %.0f%%", marker.CapacityPercent),
		"state: " + statusfmt.Lower(marker.Status),
	}
	if marker.ChargeLimitAvailable && marker.ChargeLimitPercent > 0 {
		rows = append(rows, fmt.Sprintf("charge limit: %d%%", marker.ChargeLimitPercent))
	}
	if marker.Source != "" && !strings.EqualFold(marker.Source, "unknown") {
		rows = append(rows, "source: "+strings.ToLower(marker.Source))
	}
	return tooltipModel{
		Title: "current battery · now",
		Rows:  rows,
	}
}

func gapTooltip(gap historyGapMarkerModel, location *time.Location) tooltipModel {
	title := "history gap"
	if strings.TrimSpace(gap.Label) != "" {
		title = gap.Label
	}
	return tooltipModel{
		Title: strings.ToLower(title),
		Rows: []string{
			fmt.Sprintf(
				"%s → %s",
				gap.Start.In(location).Format("02 Jan, 15:04"),
				gap.End.In(location).Format("02 Jan, 15:04"),
			),
			"no telemetry recorded",
			"no drain inferred",
		},
	}
}

func eventTooltip(marker historyEventMarkerModel, location *time.Location) tooltipModel {
	title := "event marker"
	if strings.TrimSpace(marker.Label) != "" {
		title = marker.Label
	}
	rows := []string{marker.Time.In(location).Format("02 Jan, 15:04")}
	if marker.End.After(marker.Time) {
		rows = append(rows, fmt.Sprintf("until %s", marker.End.In(location).Format("15:04")))
	}
	return tooltipModel{
		Title: strings.ToLower(title),
		Rows:  rows,
	}
}

func screenTooltip(bar dto.SOTBarDTO, day *dto.DailySummaryDTO, location *time.Location) tooltipModel {
	if day != nil {
		rows := []string{
			fmt.Sprintf("screen active: %s", formatDuration(day.ActiveDuration)),
			formatDailyDischarge(day.TotalDischarge),
			fmt.Sprintf("charged: +%.1f%%", day.TotalCharge),
			fmt.Sprintf("sleep: %s", formatDuration(day.SleepDuration)),
		}
		if day.Partial {
			rows = append(rows, "partial data")
		}
		if provenance := normalizedProvenance(day.Provenance); provenance != "" {
			rows = append(rows, provenance)
		}
		return tooltipModel{
			Title: day.Start.In(location).Format("Mon, 02 Jan"),
			Rows:  rows,
		}
	}
	return tooltipModel{
		Title: fmt.Sprintf(
			"%s → %s",
			bar.Start.In(location).Format("02 Jan 15:04"),
			bar.End.In(location).Format("15:04"),
		),
		Rows: []string{
			"screen active: " + formatDuration(bar.Duration),
			"interval: " + formatDuration(bar.End.Sub(bar.Start)),
		},
	}
}

func formatDailyDischarge(value float64) string {
	if value <= 0 {
		return "battery: 0%"
	}
	return fmt.Sprintf("battery: -%.1f%%", value)
}

func displaySessionType(value string) string {
	switch normalizedSessionType(value) {
	case "not_charging":
		return "Not charging"
	case "charging":
		return "Charging"
	case "discharging":
		return "Discharging"
	case "full":
		return "Full"
	case "sleeping":
		return "Sleep"
	default:
		return availableText(value)
	}
}
