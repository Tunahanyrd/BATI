package gui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"bati/internal/dto"
	"bati/internal/statusfmt"
)

const (
	minimumMeaningfulHistoryGap = 30 * time.Minute
	minimumMeaningfulEventSpan  = 5 * time.Minute
)

type axisTick struct {
	Time  time.Time
	Label string
}

type chargeLimitLineModel struct {
	Available bool
	Percent   int
	Label     string
}

type currentBatteryMarkerModel struct {
	Available            bool
	CapacityPercent      float64
	Status               string
	Source               string
	ChargeLimitAvailable bool
	ChargeLimitPercent   int
}

type historyEventMarkerModel struct {
	Kind  string
	Time  time.Time
	End   time.Time
	Label string
}

type historyGapMarkerModel struct {
	Start time.Time
	End   time.Time
	Label string
	Stale bool
}

type historyChartModel struct {
	DisplayStart time.Time
	DisplayEnd   time.Time
	RenderState  ChartDataState
	Segments     [][]dto.TimelinePointDTO
	BatteryTicks []axisTick
	ScreenTicks  []axisTick
	ChargeLimit  chargeLimitLineModel
	Current      currentBatteryMarkerModel
	Events       []historyEventMarkerModel
	Gaps         []historyGapMarkerModel
	MaxScreen    time.Duration
	HasCoverage  bool
	HasActivity  bool
	NoticeTitle  string
	NoticeBody   string
}

func buildHistoryChartModel(data *dto.DashboardDTO, use24h bool, location *time.Location) historyChartModel {
	timeline := data.Timeline
	start, end := ChartDisplayRange(timeline)
	segments := ContinuousSegments(timeline.Points, 15*time.Minute)
	for index := range segments {
		segments[index] = downsamplePoints(segments[index], 800)
	}
	model := historyChartModel{
		DisplayStart: start,
		DisplayEnd:   end,
		RenderState:  TimelineRenderState(timeline.Points),
		Segments:     segments,
		BatteryTicks: axisTicks(start, end, use24h, 7, location),
		ScreenTicks:  axisTicks(start, end, use24h, 5, location),
		ChargeLimit:  chargeLimitLine(data),
		Current:      currentBatteryMarker(data),
		Events:       eventMarkers(timeline),
		Gaps:         gapMarkers(data),
	}
	if !use24h {
		model.ScreenTicks = axisTicks(timeline.Start, timeline.End, false, 5, location)
	}
	for _, bar := range timeline.SOTBars {
		if bar.Observed {
			model.HasCoverage = true
		}
		if bar.Duration > 0 {
			model.HasActivity = true
		}
		if bar.Duration > model.MaxScreen {
			model.MaxScreen = bar.Duration
		}
	}
	if model.MaxScreen <= 0 {
		model.MaxScreen = 30 * time.Minute
	}
	model.MaxScreen = screenAxisMaximum(model.MaxScreen)

	hist := data.HistoricalSnapshot
	live := data.LiveSnapshot
	if hist.Available && hist.Stale {
		model.NoticeTitle = fmt.Sprintf("History ended %s ago.", formatAge(hist.Age))
		if live.Available {
			var parts []string
			parts = append(parts, fmt.Sprintf("Current battery is %.0f%%", live.CapacityPercent))
			parts = append(parts, statusfmt.Lower(live.Status))
			if live.ChargeLimitAvailable && live.ChargeLimitPercent > 0 {
				parts = append(parts, fmt.Sprintf("charge limit %d%%", live.ChargeLimitPercent))
			}
			model.NoticeBody = strings.Join(parts, " · ") + "."
		} else {
			model.NoticeBody = "No current battery snapshot."
		}
	} else {
		selectedDuration := timeline.End.Sub(timeline.Start)
		availableDuration := timeline.AvailableTo.Sub(timeline.AvailableFrom)
		switch model.RenderState {
		case ChartEmpty:
			model.NoticeTitle = "Bati is ready to build your battery history."
			model.NoticeBody = "Keep batid running. Real battery and screen activity will appear here."
		case ChartInsufficient:
			model.NoticeTitle = "Bati has its first battery sample."
			model.NoticeBody = "A second sample is needed to draw a battery trend."
		default:
			if selectedDuration > 0 && availableDuration > 0 && availableDuration < selectedDuration*3/4 {
				model.NoticeTitle = "Bati is building your battery history."
				model.NoticeBody = "Available data: " + formatDuration(availableDuration) +
					". Keep batid running to unlock full 24-hour and 10-day trends."
			}
		}
	}
	return model
}

func chargeLimitLine(data *dto.DashboardDTO) chargeLimitLineModel {
	if data.LiveSnapshot.ChargeLimitAvailable && data.LiveSnapshot.ChargeLimitPercent > 0 {
		percent := data.LiveSnapshot.ChargeLimitPercent
		return chargeLimitLineModel{
			Available: true,
			Percent:   percent,
			Label:     fmt.Sprintf("charge limit %d%%", percent),
		}
	}
	if data.Health.ChargeLimitAvailable && data.Health.ChargeLimitPercent > 0 {
		percent := data.Health.ChargeLimitPercent
		return chargeLimitLineModel{
			Available: true,
			Percent:   percent,
			Label:     fmt.Sprintf("charge limit %d%%", percent),
		}
	}
	return chargeLimitLineModel{}
}

func currentBatteryMarker(data *dto.DashboardDTO) currentBatteryMarkerModel {
	live := data.LiveSnapshot
	hist := data.HistoricalSnapshot
	if !live.Available || !hist.Available || !hist.Stale {
		return currentBatteryMarkerModel{}
	}
	return currentBatteryMarkerModel{
		Available:            true,
		CapacityPercent:      live.CapacityPercent,
		Status:               live.Status,
		Source:               availableText(live.Source),
		ChargeLimitAvailable: live.ChargeLimitAvailable,
		ChargeLimitPercent:   live.ChargeLimitPercent,
	}
}

func eventMarkers(timeline dto.TimelineDTO) []historyEventMarkerModel {
	events := make([]historyEventMarkerModel, 0, len(timeline.Sessions)*2)
	for _, session := range timeline.Sessions {
		if session.EndTime.Sub(session.StartTime) < minimumMeaningfulEventSpan {
			continue
		}
		sessionType := normalizedSessionType(session.Type)
		switch sessionType {
		case "sleeping":
			events = append(events,
				historyEventMarkerModel{Kind: "sleep_start", Time: session.StartTime, End: session.EndTime, Label: "sleep"},
				historyEventMarkerModel{Kind: "resume", Time: session.EndTime, Label: "resume"},
			)
		case "charging":
			events = append(events,
				historyEventMarkerModel{Kind: "charge_start", Time: session.StartTime, End: session.EndTime, Label: "charge start"},
				historyEventMarkerModel{Kind: "charge_end", Time: session.EndTime, Label: "charge end"},
			)
		}
	}
	return events
}

func gapMarkers(data *dto.DashboardDTO) []historyGapMarkerModel {
	points := data.Timeline.Points
	gaps := make([]historyGapMarkerModel, 0)
	for index := 1; index < len(points); index++ {
		start := points[index-1].Timestamp
		end := points[index].Timestamp
		if end.Sub(start) >= minimumMeaningfulHistoryGap {
			gaps = append(gaps, historyGapMarkerModel{
				Start: start,
				End:   end,
				Label: "history gap",
			})
		}
	}
	hist := data.HistoricalSnapshot
	live := data.LiveSnapshot
	if hist.Available && hist.Stale {
		end := hist.Timestamp.Add(hist.Age)
		if live.Available && live.Timestamp.After(hist.Timestamp) {
			end = live.Timestamp
		}
		if end.After(hist.Timestamp) {
			gaps = append(gaps, historyGapMarkerModel{
				Start: hist.Timestamp,
				End:   end,
				Label: "history gap",
				Stale: true,
			})
		}
	}
	return gaps
}

func downsamplePoints(points []dto.TimelinePointDTO, maximum int) []dto.TimelinePointDTO {
	if maximum < 2 || len(points) <= maximum {
		return points
	}
	result := make([]dto.TimelinePointDTO, 0, maximum)
	result = append(result, points[0])
	interior := maximum - 2
	for index := 1; index <= interior; index++ {
		source := 1 + index*(len(points)-2)/(interior+1)
		result = append(result, points[source])
	}
	result = append(result, points[len(points)-1])
	return result
}

func axisTicks(start, end time.Time, use24h bool, count int, location *time.Location) []axisTick {
	if count < 2 || !end.After(start) {
		return nil
	}
	format := "15:04"
	if !use24h {
		format = "02 Jan"
		if end.Sub(start) <= 36*time.Hour {
			format = "02 Jan 15:04"
			count = min(count, 4)
		}
	}
	ticks := make([]axisTick, 0, count)
	seen := make(map[string]struct{}, count)
	for index := 0; index < count; index++ {
		ratio := float64(index) / float64(count-1)
		at := start.Add(time.Duration(float64(end.Sub(start)) * ratio))
		label := at.In(location).Format(format)
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		ticks = append(ticks, axisTick{Time: at, Label: label})
	}
	return ticks
}

func nearestPointIndex(points []dto.TimelinePointDTO, target time.Time) int {
	if len(points) == 0 {
		return -1
	}
	index := sort.Search(len(points), func(index int) bool {
		return !points[index].Timestamp.Before(target)
	})
	switch {
	case index == 0:
		return 0
	case index == len(points):
		return len(points) - 1
	default:
		before := points[index-1].Timestamp
		after := points[index].Timestamp
		if target.Sub(before) <= after.Sub(target) {
			return index - 1
		}
		return index
	}
}
