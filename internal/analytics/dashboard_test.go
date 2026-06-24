package analytics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"bati/internal/db"
	"bati/internal/dto"
	"bati/internal/model"

	_ "modernc.org/sqlite"
)

func TestFetchDashboardData(t *testing.T) {
	// Create a temp database
	tempDir, err := os.MkdirTemp("", "bati-test-dashboard-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	if err := database.InitSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	// Seed battery hardware info
	battery := model.Device{
		ID:             "BAT0",
		Vendor:         "Asus",
		Model:          "Main Battery",
		DesignCapacity: 50.0,
		FullCapacity:   45.0,
		Technology:     "LION",
		CycleCount:     120,
		IsPowerSupply:  true,
		FirstSeen:      time.Now(),
		LastSeen:       time.Now(),
	}
	if err := database.SaveDevice(battery); err != nil {
		t.Fatalf("failed to save device: %v", err)
	}

	// Seed timeline telemetry:
	// 24 hours lookback range
	now := time.Now().UTC()
	baseTime := now.Add(-24 * time.Hour)

	// We seed 6 telemetry points (one every 4 hours)
	telemetryData := []model.Telemetry{
		{Timestamp: baseTime, Capacity: 90.0, Status: "Discharging", EnergyRate: 5.0, Voltage: 15.4, ScreenOn: true},
		{Timestamp: baseTime.Add(4 * time.Hour), Capacity: 80.0, Status: "Discharging", EnergyRate: 5.0, Voltage: 15.3, ScreenOn: true},
		{Timestamp: baseTime.Add(8 * time.Hour), Capacity: 70.0, Status: "Discharging", EnergyRate: 5.0, Voltage: 15.2, ScreenOn: false},
		{Timestamp: baseTime.Add(12 * time.Hour), Capacity: 60.0, Status: "Charging", EnergyRate: 15.0, Voltage: 15.5, ScreenOn: false},
		{Timestamp: baseTime.Add(16 * time.Hour), Capacity: 80.0, Status: "Charging", EnergyRate: 15.0, Voltage: 15.6, ScreenOn: false},
		{Timestamp: baseTime.Add(20 * time.Hour), Capacity: 95.0, Status: "Full", EnergyRate: 0.0, Voltage: 16.8, ScreenOn: true},
	}

	if err := database.SaveTelemetryBatch(telemetryData); err != nil {
		t.Fatalf("failed to save telemetry: %v", err)
	}

	// Fetch dashboard data for 24h view
	dashboard, err := FetchDashboardData(database, now, true)
	if err != nil {
		t.Fatalf("FetchDashboardData failed: %v", err)
	}

	// 1. Verify battery status mapping
	if dashboard.Status.Capacity != 95.0 {
		t.Errorf("expected latest capacity 95.0, got %f", dashboard.Status.Capacity)
	}
	if dashboard.Status.Status != "Full" {
		t.Errorf("expected latest status 'Full', got %s", dashboard.Status.Status)
	}

	// 2. Verify health card mapping
	if dashboard.Health.Model != "Main Battery" {
		t.Errorf("expected health model 'Main Battery', got %s", dashboard.Health.Model)
	}
	if dashboard.Health.HealthPct != 90.0 { // 45.0 / 50.0 * 100
		t.Errorf("expected health percentage 90.0, got %f", dashboard.Health.HealthPct)
	}
	if dashboard.Health.CycleCount != 120 {
		t.Errorf("expected cycle count 120, got %d", dashboard.Health.CycleCount)
	}

	// 3. Verify timeline mapping
	if len(dashboard.Timeline.Points) != 6 {
		t.Errorf("expected 6 timeline points, got %d", len(dashboard.Timeline.Points))
	}
	if !dashboard.Timeline.AvailableFrom.Equal(telemetryData[0].Timestamp) {
		t.Errorf("expected available range to start at first point")
	}
	if !dashboard.Timeline.AvailableTo.Equal(telemetryData[len(telemetryData)-1].Timestamp) {
		t.Errorf("expected available range to end at last point")
	}

	// 4. Verify SOT aggregation bars
	if len(dashboard.Timeline.SOTBars) != 48 {
		t.Errorf("expected 48 SOT bars for 24h view, got %d", len(dashboard.Timeline.SOTBars))
	}

	// At 20 hours (index 5) screen was on. Verify that active duration is calculated
	var totalActiveSOT time.Duration
	for _, bar := range dashboard.Timeline.SOTBars {
		totalActiveSOT += bar.Duration
	}
	if totalActiveSOT == 0 {
		t.Errorf("expected non-zero active SOT across the daily bars")
	}

	// Test 10 day view
	dashboard10d, err := FetchDashboardData(database, now, false)
	if err != nil {
		t.Fatalf("FetchDashboardData 10d failed: %v", err)
	}

	if len(dashboard10d.Timeline.SOTBars) != 10 {
		t.Errorf("expected 10 SOT bars for 10d view, got %d", len(dashboard10d.Timeline.SOTBars))
	}
}

func TestBuildTimelineDTOAggregation(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	points := []model.Telemetry{
		{Timestamp: start.Add(30 * time.Minute), ScreenOn: true},
		{Timestamp: start.Add(35 * time.Minute), ScreenOn: false},
		{Timestamp: start.Add(25*time.Hour + 10*time.Minute), ScreenOn: true},
	}

	timeline24 := BuildTimelineDTO(points[:2], nil, start, start.Add(24*time.Hour), true)
	if len(timeline24.SOTBars) != 48 {
		t.Fatalf("expected 48 half-hour bars, got %d", len(timeline24.SOTBars))
	}
	if got := timeline24.SOTBars[1].Duration; got != 5*time.Minute {
		t.Fatalf("expected second half-hour bar to contain 5m, got %s", got)
	}

	timeline10d := BuildTimelineDTO(points, nil, start, start.Add(10*24*time.Hour), false)
	if len(timeline10d.SOTBars) != 10 {
		t.Fatalf("expected 10 daily bars, got %d", len(timeline10d.SOTBars))
	}
	if got := timeline10d.SOTBars[0].Duration; got != 5*time.Minute {
		t.Fatalf("expected first daily bar to contain 5m, got %s", got)
	}
	if got := timeline10d.SOTBars[1].Duration; got != 5*time.Minute {
		t.Fatalf("expected second daily bar to contain 5m, got %s", got)
	}
}

func TestBuildTimelineDTOFiltersInvalidCapacity(t *testing.T) {
	start := time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC)
	timeline := BuildTimelineDTO([]model.Telemetry{
		{Timestamp: start, Capacity: 85, Status: "Discharging"},
		{Timestamp: start.Add(time.Minute), Capacity: 1693139, Status: "Full"},
		{Timestamp: start.Add(2 * time.Minute), Capacity: 84, Status: "Discharging"},
	}, nil, start, start.Add(2*time.Minute), true)

	if len(timeline.Points) != 2 {
		t.Fatalf("expected invalid capacity to be filtered from timeline, got %+v", timeline.Points)
	}
	if !timeline.AvailableFrom.Equal(start) || !timeline.AvailableTo.Equal(start.Add(2*time.Minute)) {
		t.Fatalf("unexpected timeline availability: %+v", timeline)
	}
	for _, point := range timeline.Points {
		if point.Capacity < 0 || point.Capacity > 100 {
			t.Fatalf("timeline kept invalid capacity: %+v", point)
		}
	}
}

func TestFetchDashboardDataEmptyDatabase(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.Open(filepath.Join(tempDir, "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitSchema(); err != nil {
		t.Fatal(err)
	}

	dashboard, err := FetchDashboardData(database, time.Now(), true)
	if err != nil {
		t.Fatalf("empty database should be renderable: %v", err)
	}
	if dashboard.Status.Available {
		t.Fatal("status must be unavailable without telemetry")
	}
	if len(dashboard.Timeline.Points) != 0 {
		t.Fatal("empty timeline must not contain placeholder points")
	}
}

func TestScreenBarsDistinguishObservedZeroFromMissing(t *testing.T) {
	start := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	timeline := BuildTimelineDTO([]model.Telemetry{
		{Timestamp: start.Add(10 * time.Minute), ScreenOn: false},
	}, nil, start, start.Add(24*time.Hour), true)
	if !timeline.SOTBars[0].Observed {
		t.Fatal("screen-off telemetry should mark its interval as observed")
	}
	if timeline.SOTBars[0].Duration != 0 {
		t.Fatalf("observed screen-off interval must remain zero active time, got %s", timeline.SOTBars[0].Duration)
	}
	if timeline.SOTBars[1].Observed {
		t.Fatal("a bin without telemetry coverage must remain missing, not zero")
	}
}

func TestTenDayBarsUseLocalCalendarDaysAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}
	previous := time.Local
	time.Local = location
	defer func() { time.Local = previous }()

	localStart := time.Date(2026, 3, 7, 0, 0, 0, 0, location)
	localEnd := localStart.AddDate(0, 0, 3)
	timeline := BuildTimelineDTO(nil, nil, localStart.UTC(), localEnd.UTC(), false)
	if len(timeline.SOTBars) != 3 {
		t.Fatalf("expected three local calendar days, got %d", len(timeline.SOTBars))
	}
	want := []time.Duration{24 * time.Hour, 23 * time.Hour, 24 * time.Hour}
	for index, bar := range timeline.SOTBars {
		if got := bar.End.Sub(bar.Start); got != want[index] {
			t.Fatalf("day %d duration=%s, want %s", index, got, want[index])
		}
		if bar.Start.Location() != time.UTC || bar.End.Location() != time.UTC {
			t.Fatalf("day %d boundaries must remain UTC internally: %s - %s", index, bar.Start, bar.End)
		}
	}
}

func TestDailySummarySplitsSessionsAcrossLocalMidnight(t *testing.T) {
	location := time.FixedZone("UTC+3", 3*60*60)
	previous := time.Local
	time.Local = location
	defer func() { time.Local = previous }()

	localStart := time.Date(2026, 6, 21, 0, 0, 0, 0, location)
	start := localStart.UTC()
	end := localStart.AddDate(0, 0, 2).UTC()
	sleepStart := time.Date(2026, 6, 21, 23, 0, 0, 0, location).UTC()
	sleepEnd := time.Date(2026, 6, 22, 7, 0, 0, 0, location).UTC()
	timeline := BuildTimelineDTO([]model.Telemetry{
		{Timestamp: sleepStart.Add(-10 * time.Minute), ScreenOn: true},
		{Timestamp: sleepEnd.Add(10 * time.Minute), ScreenOn: true},
	}, []model.Session{
		{
			StartTime: sleepStart, EndTime: sleepEnd, Type: "sleeping",
			StartPct: 80, EndPct: 76, DeltaPct: -4, Duration: 8 * time.Hour,
			Provenance: "observed",
		},
	}, start, end, false)
	if len(timeline.Days) != 2 {
		t.Fatalf("expected two daily summaries, got %d", len(timeline.Days))
	}
	if timeline.Days[0].SleepDuration != time.Hour {
		t.Fatalf("first day should contain one hour of sleep, got %s", timeline.Days[0].SleepDuration)
	}
	if timeline.Days[1].SleepDuration != 7*time.Hour {
		t.Fatalf("second day should contain seven hours of sleep, got %s", timeline.Days[1].SleepDuration)
	}
	if timeline.Days[0].TotalDischarge != 0.5 || timeline.Days[1].TotalDischarge != 3.5 {
		t.Fatalf("sleep drain should be split proportionally: %+v", timeline.Days)
	}
}

func TestBuildRangeSummaryDTO(t *testing.T) {
	summary := &DailySummary{
		TotalDischarge: 18,
		TotalCharge:    4,
		ActiveDuration: 6*time.Hour + 42*time.Minute,
		SleepDuration:  7 * time.Hour,
		Provenance:     "observed",
		Sessions: []model.Session{
			{Type: "charging", Duration: 2 * time.Hour, Provenance: "observed"},
		},
	}
	start := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	got := buildRangeSummaryDTO(summary, dto.TimelineDTO{
		AvailableFrom: start,
		AvailableTo:   start.Add(24 * time.Hour),
	})
	if got.TotalDischarge != 18 || got.TotalCharge != 4 ||
		got.ActiveDuration != 6*time.Hour+42*time.Minute ||
		got.ChargingDuration != 2*time.Hour ||
		got.SleepDuration != 7*time.Hour ||
		got.AvailableDuration != 24*time.Hour {
		t.Fatalf("unexpected range summary DTO: %+v", got)
	}
}

func TestHealthDTOUnavailableFieldsRemainUnknown(t *testing.T) {
	health := healthDTO(model.Device{
		Vendor:         " ",
		Model:          "",
		Technology:     "unknown",
		DesignCapacity: 0,
		FullCapacity:   0,
		CycleCount:     0,
	})
	if health.HealthAvailable {
		t.Fatal("health percentage must be unavailable without both capacities")
	}
	if health.CycleCountAvailable {
		t.Fatal("zero cycle count must not be presented as observed")
	}
	if health.Vendor != "" || health.Model != "" {
		t.Fatalf("unknown identity fields must stay empty for UI unknown handling: %+v", health)
	}
}

func TestCompleteTenDayTimelineHasTenObservedCalendarDays(t *testing.T) {
	location := time.FixedZone("UTC+3", 3*60*60)
	previous := time.Local
	time.Local = location
	defer func() { time.Local = previous }()

	localStart := time.Date(2026, 6, 1, 0, 0, 0, 0, location)
	localEnd := localStart.AddDate(0, 0, 10)
	points := make([]model.Telemetry, 0, 10*24*2)
	for at := localStart; at.Before(localEnd); at = at.Add(30 * time.Minute) {
		points = append(points, model.Telemetry{
			Timestamp: at.UTC(),
			Capacity:  80,
			Status:    "Discharging",
			ScreenOn:  true,
		})
	}
	timeline := BuildTimelineDTO(points, nil, localStart.UTC(), localEnd.UTC(), false)
	if len(timeline.Days) != 10 {
		t.Fatalf("expected 10 daily summaries, got %d", len(timeline.Days))
	}
	for index, day := range timeline.Days {
		if !day.Observed {
			t.Fatalf("day %d should be observed", index)
		}
		if day.ActiveDuration == 0 {
			t.Fatalf("day %d should include real active duration", index)
		}
	}
}
