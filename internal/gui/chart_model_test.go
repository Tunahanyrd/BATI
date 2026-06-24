package gui

import (
	"strings"
	"testing"
	"time"

	"bati/internal/dto"
	"bati/internal/model"
)

func TestHistoryModelSparseStates(t *testing.T) {
	start := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		points    []dto.TimelinePointDTO
		available time.Duration
		wantState ChartDataState
		wantText  string
	}{
		{name: "no telemetry", wantState: ChartEmpty, wantText: "ready to build"},
		{
			name:      "one telemetry point",
			points:    []dto.TimelinePointDTO{{Timestamp: start.Add(23 * time.Hour)}},
			wantState: ChartInsufficient, wantText: "first battery sample",
		},
		{
			name: "thirty minutes",
			points: []dto.TimelinePointDTO{
				{Timestamp: start.Add(23 * time.Hour)},
				{Timestamp: start.Add(23*time.Hour + 30*time.Minute)},
			},
			available: 30 * time.Minute, wantState: ChartReady, wantText: "Available data: 30m",
		},
		{
			name: "four hours",
			points: []dto.TimelinePointDTO{
				{Timestamp: start.Add(19 * time.Hour)},
				{Timestamp: start.Add(23 * time.Hour)},
			},
			available: 4 * time.Hour, wantState: ChartReady, wantText: "Available data: 4h",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			timeline := dto.TimelineDTO{
				Start: start, End: start.Add(24 * time.Hour), Points: test.points,
			}
			if len(test.points) > 0 {
				timeline.AvailableFrom = test.points[0].Timestamp
				timeline.AvailableTo = test.points[len(test.points)-1].Timestamp
			}
			data := &dto.DashboardDTO{Timeline: timeline}
			model := buildHistoryChartModel(data, true, time.UTC)
			if model.RenderState != test.wantState {
				t.Fatalf("state=%v, want %v", model.RenderState, test.wantState)
			}
			copy := model.NoticeTitle + " " + model.NoticeBody
			if !strings.Contains(copy, test.wantText) {
				t.Fatalf("notice %q does not contain %q", copy, test.wantText)
			}
		})
	}
}

func TestHistoryModelFull24HoursHasNoLimitedNotice(t *testing.T) {
	start := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	timeline := dto.TimelineDTO{
		Start: start, End: start.Add(24 * time.Hour),
		AvailableFrom: start, AvailableTo: start.Add(24 * time.Hour),
		Points: []dto.TimelinePointDTO{
			{Timestamp: start, Capacity: 90},
			{Timestamp: start.Add(24 * time.Hour), Capacity: 70},
		},
	}
	data := &dto.DashboardDTO{Timeline: timeline}
	model := buildHistoryChartModel(data, true, time.UTC)
	if model.NoticeTitle != "" || model.NoticeBody != "" {
		t.Fatalf("full range should not show limited-data notice: %+v", model)
	}
}

func TestHistoryModelStaleNoticeUsesLiveSnapshot(t *testing.T) {
	start := time.Date(2026, 6, 21, 16, 34, 0, 0, time.UTC)
	latest := start.Add(24 * time.Hour)
	timeline := dto.TimelineDTO{
		Start: start, End: latest,
		AvailableFrom: latest.Add(-4 * time.Hour),
		AvailableTo:   latest,
		Points: []dto.TimelinePointDTO{
			{Timestamp: latest.Add(-4 * time.Hour), Capacity: 100, Status: "Full"},
			{Timestamp: latest, Capacity: 100, Status: "Full"},
		},
	}
	data := &dto.DashboardDTO{
		Timeline: timeline,
		HistoricalSnapshot: dto.HistoricalSnapshotDTO{
			Available: true, Stale: true, Age: 20 * time.Hour,
			Timestamp: latest, CapacityPercent: 100, Status: "Full",
		},
		LiveSnapshot: dto.LiveSnapshotDTO{
			Available: true, CapacityPercent: 93, Status: "Not charging",
			ChargeLimitAvailable: true, ChargeLimitPercent: 80,
		},
	}

	model := buildHistoryChartModel(data, true, time.UTC)
	if model.NoticeTitle != "History ended 20h ago." {
		t.Fatalf("unexpected stale notice title: %q", model.NoticeTitle)
	}
	if model.NoticeBody != "Current battery is 93% · not charging · charge limit 80%." {
		t.Fatalf("unexpected stale notice body: %q", model.NoticeBody)
	}
	if !model.ChargeLimit.Available || model.ChargeLimit.Percent != 80 {
		t.Fatalf("expected charge limit model at 80%%, got %+v", model.ChargeLimit)
	}
	if model.ChargeLimit.Label != "charge limit 80%" {
		t.Fatalf("expected full charge limit label, got %q", model.ChargeLimit.Label)
	}
	if !model.Current.Available || model.Current.CapacityPercent != 93 {
		t.Fatalf("expected separate current marker at 93%%, got %+v", model.Current)
	}
	for _, segment := range model.Segments {
		for _, point := range segment {
			if point.Capacity == data.LiveSnapshot.CapacityPercent {
				t.Fatalf("live battery %v%% must not be inserted into historical chart segment: %+v", data.LiveSnapshot.CapacityPercent, segment)
			}
		}
	}
	if len(model.Gaps) == 0 || !model.Gaps[len(model.Gaps)-1].Stale {
		t.Fatalf("expected stale history gap marker, got %+v", model.Gaps)
	}
}

func TestHistoryModelInternalGapMarker(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	timeline := dto.TimelineDTO{
		Start: start, End: start.Add(3 * time.Hour),
		Points: []dto.TimelinePointDTO{
			{Timestamp: start, Capacity: 80},
			{Timestamp: start.Add(10 * time.Minute), Capacity: 79},
			{Timestamp: start.Add(50 * time.Minute), Capacity: 78},
		},
	}
	data := &dto.DashboardDTO{Timeline: timeline}
	model := buildHistoryChartModel(data, true, time.UTC)
	if len(model.Gaps) != 1 {
		t.Fatalf("expected one internal gap marker, got %+v", model.Gaps)
	}
	if model.Gaps[0].Label != "history gap" || !model.Gaps[0].Start.Equal(start.Add(10*time.Minute)) || !model.Gaps[0].End.Equal(start.Add(50*time.Minute)) {
		t.Fatalf("unexpected gap marker: %+v", model.Gaps[0])
	}
}

func TestHistoryModelIgnoresShortGapMarker(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	timeline := dto.TimelineDTO{
		Start: start, End: start.Add(time.Hour),
		Points: []dto.TimelinePointDTO{
			{Timestamp: start, Capacity: 80},
			{Timestamp: start.Add(10 * time.Minute), Capacity: 79},
			{Timestamp: start.Add(25 * time.Minute), Capacity: 78},
		},
	}
	data := &dto.DashboardDTO{Timeline: timeline}
	model := buildHistoryChartModel(data, true, time.UTC)
	if len(model.Gaps) != 0 {
		t.Fatalf("short telemetry pauses should not become visible gap markers: %+v", model.Gaps)
	}
}

func TestHistoryModelEventMarkers(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	timeline := dto.TimelineDTO{
		Start: start, End: start.Add(2 * time.Hour),
		Sessions: []model.Session{
			{StartTime: start, EndTime: start.Add(time.Hour), Type: "charging"},
			{StartTime: start.Add(time.Hour), EndTime: start.Add(2 * time.Hour), Type: "sleeping"},
		},
	}
	data := &dto.DashboardDTO{Timeline: timeline}
	model := buildHistoryChartModel(data, true, time.UTC)
	var chargeStart, resume bool
	for _, marker := range model.Events {
		if marker.Kind == "charge_start" {
			chargeStart = true
		}
		if marker.Kind == "resume" {
			resume = true
		}
	}
	if !chargeStart || !resume {
		t.Fatalf("expected charge start and resume event markers, got %+v", model.Events)
	}
}

func TestHistoryModelSuppressesRoutineStateEventClusters(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	sessions := make([]model.Session, 0, 20)
	for index := 0; index < 20; index++ {
		sessionStart := start.Add(time.Duration(index) * 5 * time.Minute)
		sessions = append(sessions, model.Session{
			StartTime: sessionStart,
			EndTime:   sessionStart.Add(5 * time.Minute),
			Type:      "full",
		})
	}
	data := &dto.DashboardDTO{Timeline: dto.TimelineDTO{
		Start: start, End: start.Add(2 * time.Hour), Sessions: sessions,
	}}
	model := buildHistoryChartModel(data, true, time.UTC)
	if len(model.Events) != 0 {
		t.Fatalf("routine full-session clusters should not create unreadable event ticks: %+v", model.Events)
	}
}

func TestTenDaySparseAxisLabelsRemainUnique(t *testing.T) {
	start := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	timeline := dto.TimelineDTO{
		Start: start, End: start.Add(10 * 24 * time.Hour),
		AvailableFrom: start.Add(9 * 24 * time.Hour),
		AvailableTo:   start.Add(9*24*time.Hour + 4*time.Hour),
		Points: []dto.TimelinePointDTO{
			{Timestamp: start.Add(9 * 24 * time.Hour)},
			{Timestamp: start.Add(9*24*time.Hour + 4*time.Hour)},
		},
	}
	data := &dto.DashboardDTO{Timeline: timeline}
	model := buildHistoryChartModel(data, false, time.UTC)
	seen := map[string]bool{}
	for _, tick := range model.BatteryTicks {
		if seen[tick.Label] {
			t.Fatalf("repeated 10-day sparse label %q", tick.Label)
		}
		seen[tick.Label] = true
	}
	if !strings.Contains(model.NoticeBody, "Available data: 4h") {
		t.Fatalf("sparse 10-day notice missing duration: %q", model.NoticeBody)
	}
}

func BenchmarkNearestPointLookup(b *testing.B) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	points := make([]dto.TimelinePointDTO, 30*24*12)
	for index := range points {
		points[index].Timestamp = start.Add(time.Duration(index) * 5 * time.Minute)
	}
	target := start.Add(17*24*time.Hour + 3*time.Hour)
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = nearestPointIndex(points, target)
	}
}

func BenchmarkChartModelThirtyDays(b *testing.B) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	points := make([]dto.TimelinePointDTO, 30*24*12)
	for index := range points {
		points[index] = dto.TimelinePointDTO{
			Timestamp: start.Add(time.Duration(index) * 5 * time.Minute),
			Capacity:  float64(100 - index%80),
		}
	}
	timeline := dto.TimelineDTO{
		Start: start, End: start.Add(30 * 24 * time.Hour),
		AvailableFrom: points[0].Timestamp, AvailableTo: points[len(points)-1].Timestamp,
		Points: points,
	}
	b.ResetTimer()
	data := &dto.DashboardDTO{Timeline: timeline}
	for index := 0; index < b.N; index++ {
		_ = buildHistoryChartModel(data, false, time.UTC)
	}
}
