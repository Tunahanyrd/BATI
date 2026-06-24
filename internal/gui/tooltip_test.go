package gui

import (
	"image"
	"strings"
	"testing"
	"time"

	"bati/internal/dto"
	"bati/internal/model"
)

func TestTooltipPlacementFlipsAtEveryEdge(t *testing.T) {
	bounds := image.Rect(0, 0, 300, 180)
	size := image.Pt(100, 60)
	anchors := []image.Point{
		{X: 2, Y: 2},
		{X: 298, Y: 2},
		{X: 2, Y: 178},
		{X: 298, Y: 178},
	}
	for _, anchor := range anchors {
		got := placeTooltip(anchor, bounds, size, 12)
		if !got.In(bounds) {
			t.Fatalf("tooltip for anchor %v escaped bounds: %v", anchor, got)
		}
		if anchor.In(got) {
			t.Fatalf("tooltip should avoid covering anchor %v: %v", anchor, got)
		}
	}
}

func TestSleepTooltipUsesIntervalSemantics(t *testing.T) {
	start := time.Date(2026, 6, 22, 1, 14, 0, 0, time.UTC)
	model := sessionTooltip(model.Session{
		StartTime:  start,
		EndTime:    start.Add(7*time.Hour + 28*time.Minute),
		Type:       "sleeping",
		StartPct:   78,
		EndPct:     74,
		DeltaPct:   -4,
		Duration:   7*time.Hour + 28*time.Minute,
		Provenance: "observed",
	}, start, time.UTC)
	joined := strings.Join(append([]string{model.Title}, model.Rows...), "\n")
	for _, expected := range []string{"sleep", "01:14 → 08:42", "7h28m", "battery: 78% → 74%", "drain: -4.0%", "observed"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("sleep tooltip missing %q:\n%s", expected, joined)
		}
	}
	if strings.Contains(joined, "screen:") || strings.Contains(joined, "marker:") {
		t.Fatalf("sleep interval tooltip contains point/marker semantics:\n%s", joined)
	}
}

func TestPointTooltipIncludesSessionSummary(t *testing.T) {
	at := time.Date(2026, 6, 22, 14, 32, 0, 0, time.UTC)
	session := model.Session{Type: "full", Duration: 72 * time.Minute}
	tooltip := pointTooltip(dto.TimelinePointDTO{
		Timestamp:  at,
		Capacity:   100,
		Status:     "Full",
		EnergyRate: 0,
		ScreenOn:   true,
	}, &session, at, time.UTC)
	joined := strings.Join(tooltip.Rows, "\n")
	for _, expected := range []string{"battery: 100%", "state: full", "rate: 0.00 W", "screen: active", "session: full for 1h12m"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("point tooltip missing %q:\n%s", expected, joined)
		}
	}
}

func TestPointTooltipFormatsDischargingAsInProgress(t *testing.T) {
	at := time.Date(2026, 6, 22, 14, 32, 0, 0, time.UTC)
	tooltip := pointTooltip(dto.TimelinePointDTO{
		Timestamp: at,
		Capacity:  71,
		Status:    "Discharging",
	}, nil, at, time.UTC)
	joined := strings.Join(tooltip.Rows, "\n")
	if !strings.Contains(joined, "state: discharging...") {
		t.Fatalf("point tooltip should show in-progress discharging state:\n%s", joined)
	}
}

func TestThresholdHoldTooltip(t *testing.T) {
	at := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	tooltip := sessionTooltip(model.Session{
		StartTime: at, EndTime: at.Add(time.Hour), Type: "not_charging",
		StartPct: 80, EndPct: 80, Duration: time.Hour, Provenance: "observed",
	}, at, time.UTC)
	joined := strings.Join(append([]string{tooltip.Title}, tooltip.Rows...), "\n")
	for _, expected := range []string{"not charging", "battery held at 80%", "plugged in", "observed"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("threshold tooltip missing %q:\n%s", expected, joined)
		}
	}
}

func TestChargingSessionTooltip(t *testing.T) {
	at := time.Date(2026, 6, 22, 14, 31, 0, 0, time.UTC)
	tooltip := sessionTooltip(model.Session{
		StartTime: at, EndTime: at.Add(31 * time.Minute), Type: "charging",
		StartPct: 71, EndPct: 80, DeltaPct: 9, Duration: 31 * time.Minute,
		Provenance: "observed",
	}, at, time.UTC)
	joined := strings.Join(append([]string{tooltip.Title}, tooltip.Rows...), "\n")
	for _, expected := range []string{"charging", "14:31 → 15:02", "71% → 80%", "+9.0% over 31m", "observed"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("charging tooltip missing %q:\n%s", expected, joined)
		}
	}
}

func TestChargeLimitTooltipSeparatesLimitFromCurrentCharge(t *testing.T) {
	tooltip := chargeLimitTooltip(
		chargeLimitLineModel{Available: true, Percent: 80},
		dto.LiveSnapshotDTO{Available: true, CapacityPercent: 93},
	)
	joined := strings.Join(append([]string{tooltip.Title}, tooltip.Rows...), "\n")
	for _, expected := range []string{"charge limit · 80%", "configured end threshold", "current battery: 93%"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("charge limit tooltip missing %q:\n%s", expected, joined)
		}
	}
}

func TestCurrentBatteryTooltipUsesLiveSnapshotCopy(t *testing.T) {
	tooltip := currentBatteryTooltip(currentBatteryMarkerModel{
		Available: true, CapacityPercent: 93, Status: "Not charging",
		Source: "sysfs", ChargeLimitAvailable: true, ChargeLimitPercent: 80,
	})
	joined := strings.Join(append([]string{tooltip.Title}, tooltip.Rows...), "\n")
	for _, expected := range []string{"current battery · now", "battery: 93%", "state: not charging", "charge limit: 80%", "source: sysfs"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("current marker tooltip missing %q:\n%s", expected, joined)
		}
	}
}

func TestGapTooltipDoesNotInferDrain(t *testing.T) {
	start := time.Date(2026, 6, 22, 16, 34, 0, 0, time.UTC)
	tooltip := gapTooltip(historyGapMarkerModel{
		Start: start, End: start.Add(20 * time.Hour), Label: "history gap",
	}, time.UTC)
	joined := strings.Join(append([]string{tooltip.Title}, tooltip.Rows...), "\n")
	for _, expected := range []string{"history gap", "22 Jun, 16:34", "no telemetry recorded", "no drain inferred"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("gap tooltip missing %q:\n%s", expected, joined)
		}
	}
}

func TestScreenActivityTooltipModel(t *testing.T) {
	start := time.Date(2026, 6, 22, 16, 16, 0, 0, time.UTC)
	tooltip := screenTooltip(dto.SOTBarDTO{
		Start: start, End: start.Add(30 * time.Minute), Duration: 23 * time.Minute,
	}, nil, time.UTC)
	joined := strings.Join(append([]string{tooltip.Title}, tooltip.Rows...), "\n")
	for _, expected := range []string{"22 Jun 16:16 → 16:46", "screen active: 23m", "interval: 30m"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("screen tooltip missing %q:\n%s", expected, joined)
		}
	}
}

func TestDailyTooltipModelShowsPartialData(t *testing.T) {
	start := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	day := dto.DailySummaryDTO{
		Start: start, End: start.Add(24 * time.Hour), ActiveDuration: 5 * time.Hour,
		TotalDischarge: 32, TotalCharge: 18, SleepDuration: 7 * time.Hour,
		Partial: true, Provenance: "observed",
	}
	tooltip := screenTooltip(dto.SOTBarDTO{}, &day, time.UTC)
	joined := strings.Join(append([]string{tooltip.Title}, tooltip.Rows...), "\n")
	for _, expected := range []string{"Mon, 22 Jun", "screen active: 5h", "battery: -32.0%", "charged: +18.0%", "sleep: 7h", "partial data"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("daily tooltip missing %q:\n%s", expected, joined)
		}
	}
}

func TestPointTooltipTransitionsAndSleepScreenState(t *testing.T) {
	sStart := time.Date(2026, 6, 22, 2, 0, 0, 0, time.UTC)
	sEnd := sStart.Add(8 * time.Hour)
	sleepSession := model.Session{
		StartTime: sStart,
		EndTime:   sEnd,
		Type:      "sleeping",
		Duration:  8 * time.Hour,
	}

	// 1. Sleep Start Transition
	t1 := pointTooltip(dto.TimelinePointDTO{
		Timestamp: sStart,
		ScreenOn:  true,
	}, &sleepSession, sStart, time.UTC)
	j1 := strings.Join(t1.Rows, "\n")
	if !strings.Contains(j1, "session: sleep start for 8h") {
		t.Fatalf("expected sleep start transition, got:\n%s", j1)
	}

	// 2. Resume Transition
	t2 := pointTooltip(dto.TimelinePointDTO{
		Timestamp: sEnd,
		ScreenOn:  true,
	}, &sleepSession, sEnd, time.UTC)
	j2 := strings.Join(t2.Rows, "\n")
	if !strings.Contains(j2, "session: resume for 8h") {
		t.Fatalf("expected resume transition, got:\n%s", j2)
	}

	// 3. Inside Sleep (Screen forced Inactive)
	t3 := pointTooltip(dto.TimelinePointDTO{
		Timestamp: sStart.Add(4 * time.Hour),
		ScreenOn:  true, // telemetry says on
	}, &sleepSession, sStart.Add(4*time.Hour), time.UTC)
	j3 := strings.Join(t3.Rows, "\n")
	if !strings.Contains(j3, "screen: inactive") {
		t.Fatalf("expected screen to be overridden to inactive inside sleep, got:\n%s", j3)
	}
	if !strings.Contains(j3, "session: sleep for 8h") {
		t.Fatalf("expected plain sleep session name, got:\n%s", j3)
	}

	// 4. Charge Start Transition
	chargeSession := model.Session{
		StartTime: sStart,
		EndTime:   sStart.Add(2 * time.Hour),
		Type:      "charging",
		Duration:  2 * time.Hour,
	}
	t4 := pointTooltip(dto.TimelinePointDTO{
		Timestamp: sStart,
	}, &chargeSession, sStart, time.UTC)
	j4 := strings.Join(t4.Rows, "\n")
	if !strings.Contains(j4, "session: charge start for 2h") {
		t.Fatalf("expected charge start transition, got:\n%s", j4)
	}
}
