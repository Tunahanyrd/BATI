package gui

import (
	"image"
	"slices"
	"testing"
	"time"

	"bati/internal/dto"
	"bati/internal/model"
)

func TestViewport_MapToScreen(t *testing.T) {
	vp := Viewport{
		Width:  400,
		Height: 200,
		MinX:   1000,
		MaxX:   2000,
		MinY:   0,
		MaxY:   100,
	}

	// Midpoint (X: 50%, Y: 50%) -> Screen (200, 100) (Note: Y increases down, so Y 50% from bottom is 200-100 = 100)
	x, y := vp.MapToScreen(1500, 50)
	if x != 200 || y != 100 {
		t.Errorf("expected (200, 100), got (%v, %v)", x, y)
	}

	// Bottom-left (X: 0%, Y: 0%) -> Screen (0, 200)
	x, y = vp.MapToScreen(1000, 0)
	if x != 0 || y != 200 {
		t.Errorf("expected (0, 200), got (%v, %v)", x, y)
	}

	// Top-right (X: 100%, Y: 100%) -> Screen (400, 0)
	x, y = vp.MapToScreen(2000, 100)
	if x != 400 || y != 0 {
		t.Errorf("expected (400, 0), got (%v, %v)", x, y)
	}
}

func TestViewport_MapToData(t *testing.T) {
	vp := Viewport{
		Width:  400,
		Height: 200,
		MinX:   1000,
		MaxX:   2000,
		MinY:   0,
		MaxY:   100,
	}

	// Screen midpoint (200, 100) -> Data midpoint (1500, 50)
	x, y := vp.MapToData(200, 100)
	if x != 1500 || y != 50 {
		t.Errorf("expected (1500, 50), got (%v, %v)", x, y)
	}
}

func TestLookupClosestPoint(t *testing.T) {
	baseTime := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	points := []dto.TimelinePointDTO{
		{Timestamp: baseTime, Capacity: 90.0},
		{Timestamp: baseTime.Add(10 * time.Minute), Capacity: 88.0},
		{Timestamp: baseTime.Add(20 * time.Minute), Capacity: 86.0},
	}

	vp := Viewport{
		Width:  300,
		Height: 100,
		MinX:   float64(baseTime.Unix()),
		MaxX:   float64(baseTime.Add(20 * time.Minute).Unix()),
		MinY:   0,
		MaxY:   100,
	}

	// Point 0 is at screen X = 0 (12:00)
	// Point 1 is at screen X = 150 (12:10)
	// Point 2 is at screen X = 300 (12:20)

	// Hover near point 1 (mouseX = 148, close to 150)
	idx, sx, sy, ok := LookupClosestPoint(points, vp, 148, 10)
	if !ok || idx != 1 {
		t.Errorf("expected point 1, got idx=%d, ok=%t", idx, ok)
	}
	if sx != 150 {
		t.Errorf("expected screen X 150, got %f", sx)
	}
	// Y is capacity 88.0. 88% from bottom = height - 88 = 100 - 88 = 12
	if sy != 12 {
		t.Errorf("expected screen Y 12, got %f", sy)
	}

	// Hover too far away (mouseX = 70, closest is 0 or 150, dist = 70, tolerance = 10)
	_, _, _, ok = LookupClosestPoint(points, vp, 70, 10)
	if ok {
		t.Errorf("expected hover lookup to fail due to distance threshold")
	}
}

func TestPlotBatteryCapacityClampsInvalidDisplayValues(t *testing.T) {
	if got := plotBatteryCapacity(100.4); got != 100 {
		t.Fatalf("expected upper clamp, got %v", got)
	}
	if got := plotBatteryCapacity(-0.2); got != 0 {
		t.Fatalf("expected lower clamp, got %v", got)
	}
}

func TestTimestampAtX(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	got := TimestampAtX(start, end, 240, 60)
	want := start.Add(6 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestTimelineRenderState(t *testing.T) {
	now := time.Now()
	if got := TimelineRenderState(nil); got != ChartEmpty {
		t.Fatalf("expected empty state, got %v", got)
	}
	if got := TimelineRenderState([]dto.TimelinePointDTO{{Timestamp: now}}); got != ChartInsufficient {
		t.Fatalf("expected insufficient state, got %v", got)
	}
	if got := TimelineRenderState([]dto.TimelinePointDTO{{Timestamp: now}, {Timestamp: now.Add(time.Minute)}}); got != ChartReady {
		t.Fatalf("expected ready state, got %v", got)
	}
}

func TestChartClippingBounds(t *testing.T) {
	bounds := PlotBounds{Width: 300, Height: 100}
	x, y := bounds.ClampPoint(-20, 130)
	if x != 0 || y != 100 {
		t.Fatalf("expected point clamped to (0, 100), got (%v, %v)", x, y)
	}

	tooltip := TooltipBounds(295, -10, 300, 0, 100, 80, 40)
	want := image.Rect(203, 2, 283, 42)
	if tooltip != want {
		t.Fatalf("expected tooltip bounds %v, got %v", want, tooltip)
	}
	if tooltip.Min.X < 0 || tooltip.Min.Y < 0 || tooltip.Max.X > 300 || tooltip.Max.Y > 100 {
		t.Fatalf("tooltip escaped plot bounds: %v", tooltip)
	}
}

func TestUpdateHoverContextSharesHoverTimeAcrossChartLayers(t *testing.T) {
	start := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	hoverAt := start.Add(18 * time.Minute)
	viewport := Viewport{
		Width:  300,
		Height: 120,
		MinX:   float64(start.Unix()),
		MaxX:   float64(start.Add(30 * time.Minute).Unix()),
		MinY:   0,
		MaxY:   100,
	}
	hoverX, _ := viewport.MapToScreen(float64(hoverAt.Unix()), 0)
	state := &UIState{data: &dto.DashboardDTO{
		Timeline: dto.TimelineDTO{
			Points: []dto.TimelinePointDTO{
				{Timestamp: start.Add(15 * time.Minute), Capacity: 100, Status: "Full"},
				{Timestamp: start.Add(20 * time.Minute), Capacity: 100, Status: "Full"},
			},
			Sessions: []model.Session{{
				StartTime: start, EndTime: start.Add(30 * time.Minute),
				Type: "full", Duration: 30 * time.Minute,
			}},
			SOTBars: []dto.SOTBarDTO{{
				Start: start, End: start.Add(30 * time.Minute),
				Duration: 30 * time.Minute, Observed: true,
			}},
		},
	}}

	state.updateHoverContext(hoverX, 40, true, viewport, 100)
	batteryHover := state.hoverContext
	if !batteryHover.BatteryPlotHit || batteryHover.StateStripHit || batteryHover.ScreenChartHit {
		t.Fatalf("expected battery plot hover flags, got %+v", batteryHover)
	}
	if !batteryHover.HoverTime.Equal(hoverAt) {
		t.Fatalf("expected battery hover time %s, got %s", hoverAt, batteryHover.HoverTime)
	}
	if batteryHover.NearestBatterySample == nil || !batteryHover.NearestBatterySample.Timestamp.Equal(start.Add(20*time.Minute)) {
		t.Fatalf("expected nearest battery sample at 14:20, got %+v", batteryHover.NearestBatterySample)
	}
	if batteryHover.ContainingBatterySession == nil {
		t.Fatal("expected containing battery session for battery hover")
	}

	state.updateHoverContext(hoverX, 110, true, viewport, 100)
	stateStripHover := state.hoverContext
	if !stateStripHover.StateStripHit || stateStripHover.BatteryPlotHit || stateStripHover.ScreenChartHit {
		t.Fatalf("expected state strip hover flags, got %+v", stateStripHover)
	}
	if !stateStripHover.HoverTime.Equal(batteryHover.HoverTime) {
		t.Fatalf("state strip hover time %s did not match battery plot time %s", stateStripHover.HoverTime, batteryHover.HoverTime)
	}
	if stateStripHover.ContainingBatterySession == nil {
		t.Fatal("expected containing battery session for state strip hover")
	}

	state.updateHoverContext(hoverX, 40, false, viewport, 0)
	screenHover := state.hoverContext
	if !screenHover.ScreenChartHit || screenHover.BatteryPlotHit || screenHover.StateStripHit {
		t.Fatalf("expected screen hover flags, got %+v", screenHover)
	}
	if !screenHover.HoverTime.Equal(batteryHover.HoverTime) {
		t.Fatalf("screen hover time %s did not match battery plot time %s", screenHover.HoverTime, batteryHover.HoverTime)
	}
	if screenHover.ScreenActivityBin == nil {
		t.Fatal("expected matching screen activity bin for screen hover")
	}
}

func TestUpdateHoverContextDetectsChargeLimitAndCurrentMarker(t *testing.T) {
	start := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	hoverAt := start.Add(18 * time.Minute)
	viewport := Viewport{
		Width:  300,
		Height: 120,
		MinX:   float64(start.Unix()),
		MaxX:   float64(start.Add(30 * time.Minute).Unix()),
		MinY:   0,
		MaxY:   100,
	}
	hoverX, _ := viewport.MapToScreen(float64(hoverAt.Unix()), 0)
	state := &UIState{
		data: &dto.DashboardDTO{},
		history: historyChartModel{
			ChargeLimit: chargeLimitLineModel{Available: true, Percent: 80},
			Current: currentBatteryMarkerModel{
				Available: true, CapacityPercent: 93, Status: "Not charging",
			},
		},
	}

	_, limitY := viewport.MapToScreen(viewport.MinX, 80)
	state.updateHoverContext(hoverX, limitY, true, viewport, 100)
	if !state.hoverContext.ChargeLimitHit || state.hoverContext.BatteryPlotHit || state.hoverContext.StateStripHit {
		t.Fatalf("expected charge limit hover, got %+v", state.hoverContext)
	}
	if !state.hoverContext.HoverTime.Equal(hoverAt) {
		t.Fatalf("charge limit hover should preserve x-derived time, got %s", state.hoverContext.HoverTime)
	}

	currentX := viewport.Width - 18
	currentY := percentageY(93, viewport.Height)
	state.updateHoverContext(currentX, currentY, true, viewport, 100)
	if !state.hoverContext.CurrentMarkerHit || state.hoverContext.ChargeLimitHit {
		t.Fatalf("expected current marker hover, got %+v", state.hoverContext)
	}
}

func TestGapRegionBoundsHidesTinyNonStaleGaps(t *testing.T) {
	start := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	viewport := Viewport{
		Width:  300,
		Height: 120,
		MinX:   float64(start.Unix()),
		MaxX:   float64(start.Add(24 * time.Hour).Unix()),
		MinY:   0,
		MaxY:   100,
	}
	_, _, visible := gapRegionBounds(historyGapMarkerModel{
		Start: start.Add(time.Hour),
		End:   start.Add(time.Hour + 20*time.Minute),
	}, viewport, 300)
	if visible {
		t.Fatal("tiny non-stale gaps should not render as unreadable markers")
	}

	left, right, visible := gapRegionBounds(historyGapMarkerModel{
		Start: start.Add(23 * time.Hour),
		End:   start.Add(28 * time.Hour),
		Stale: true,
	}, viewport, 300)
	if !visible || right <= left {
		t.Fatalf("stale gap should stay visible and hoverable, got left=%v right=%v visible=%t", left, right, visible)
	}
}

func TestContinuousSegmentsDoNotBridgeSparseData(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	points := []dto.TimelinePointDTO{
		{Timestamp: start},
		{Timestamp: start.Add(5 * time.Minute)},
		{Timestamp: start.Add(2 * time.Hour)},
		{Timestamp: start.Add(2*time.Hour + 5*time.Minute)},
	}
	segments := ContinuousSegments(points, 15*time.Minute)
	if len(segments) != 2 {
		t.Fatalf("expected two continuous segments, got %d", len(segments))
	}
}

func TestOvernightEmptyState(t *testing.T) {
	value, detail := overnightDisplay(dto.OvernightDrainDTO{
		HasReport: true,
		Duration:  3*time.Hour + 59*time.Minute,
		DrainPct:  -3,
	})
	if value != "Not enough data" || detail != "Needs a 4h+ sleep or idle period" {
		t.Fatalf("unexpected short overnight display: %q, %q", value, detail)
	}
}

func TestTenDayAxisLabelsDoNotRepeatSingleAvailableDay(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.Local)
	labels := AxisLabels(start, start.Add(10*24*time.Hour), false, 7)
	if len(labels) != 7 {
		t.Fatalf("expected seven labels, got %d", len(labels))
	}
	for index, label := range labels {
		if slices.Contains(labels[:index], label) {
			t.Fatalf("10d axis repeated label %q: %v", label, labels)
		}
	}
}

func TestLookupBarAtX(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	bars := make([]dto.SOTBarDTO, 10)
	for index := range bars {
		bars[index].Start = start.Add(time.Duration(index) * time.Hour)
		bars[index].End = bars[index].Start.Add(time.Hour)
	}
	viewport := Viewport{
		Width: 500,
		MinX:  float64(start.Unix()),
		MaxX:  float64(start.Add(10 * time.Hour).Unix()),
	}
	index, left, right, ok := LookupBarAtX(bars, viewport, 4, 275)
	if !ok || index != 5 {
		t.Fatalf("expected bar 5, got index=%d ok=%t", index, ok)
	}
	if left >= right || 260 < left || 260 > right {
		t.Fatalf("invalid bar bounds: left=%f right=%f", left, right)
	}
}

func TestChartDisplayRangeFocusesSparseData(t *testing.T) {
	selectedStart := time.Date(2026, 6, 21, 17, 0, 0, 0, time.UTC)
	timeline := dto.TimelineDTO{
		Start:         selectedStart,
		End:           selectedStart.Add(24 * time.Hour),
		AvailableFrom: selectedStart.Add(19 * time.Hour),
		AvailableTo:   selectedStart.Add(23 * time.Hour),
	}
	start, end := ChartDisplayRange(timeline)
	if !start.Before(timeline.AvailableFrom) || !end.After(timeline.AvailableTo) {
		t.Fatalf("expected padding around available data, got %s - %s", start, end)
	}
	if end.Sub(start) >= 6*time.Hour {
		t.Fatalf("sparse display range remained too wide: %s", end.Sub(start))
	}
}

func TestChartDisplayRangeKeepsSelectedRangeWhenDataIsDense(t *testing.T) {
	selectedStart := time.Date(2026, 6, 21, 17, 0, 0, 0, time.UTC)
	timeline := dto.TimelineDTO{
		Start:         selectedStart,
		End:           selectedStart.Add(24 * time.Hour),
		AvailableFrom: selectedStart.Add(time.Hour),
		AvailableTo:   selectedStart.Add(23 * time.Hour),
	}
	start, end := ChartDisplayRange(timeline)
	if !start.Equal(timeline.Start) || !end.Equal(timeline.End) {
		t.Fatalf("dense data should keep selected range, got %s - %s", start, end)
	}
}

func TestLookupSessionAtPosition(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	viewport := Viewport{
		Width: 300, Height: 100,
		MinX: float64(start.Unix()), MaxX: float64(start.Add(3 * time.Hour).Unix()),
	}
	sessions := []model.Session{
		{StartTime: start.Add(time.Hour), EndTime: start.Add(2 * time.Hour), Type: "sleeping"},
		{StartTime: start.Add(2 * time.Hour), EndTime: start.Add(3 * time.Hour), Type: "charging"},
	}

	index, ok := LookupSessionAtPosition(sessions, viewport, 150, 30, 88)
	if !ok || index != 0 {
		t.Fatalf("expected sleep session, got index=%d ok=%t", index, ok)
	}

	index, ok = LookupSessionAtPosition(sessions, viewport, 250, 95, 88)
	if !ok || index != 1 {
		t.Fatalf("expected charging marker, got index=%d ok=%t", index, ok)
	}

	if _, ok = LookupSessionAtPosition(sessions, viewport, 250, 40, 88); ok {
		t.Fatal("charging marker should only be interactive near the marker band")
	}
	if index := LookupSessionAtTime(sessions, start.Add(90*time.Minute)); index != 0 {
		t.Fatalf("timestamp should resolve to sleep session 0, got %d", index)
	}
	if index := LookupSessionAtTime(sessions, start.Add(4*time.Hour)); index != -1 {
		t.Fatalf("timestamp outside sessions should not resolve, got %d", index)
	}
}

func TestScreenAxisMaximumAddsHeadroom(t *testing.T) {
	if got := screenAxisMaximum(30 * time.Minute); got != time.Hour {
		t.Fatalf("expected 60m axis for 30m data, got %s", got)
	}
	if got := screenAxisMaximum(44 * time.Minute); got != time.Hour {
		t.Fatalf("expected 60m axis for 44m data, got %s", got)
	}
}

func TestSparseDataNotice(t *testing.T) {
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	timeline := dto.TimelineDTO{
		Start:         start,
		End:           start.Add(10 * 24 * time.Hour),
		AvailableFrom: start.Add(9 * 24 * time.Hour),
		AvailableTo:   start.Add(9*24*time.Hour + 4*time.Hour + 18*time.Minute),
	}
	got := SparseDataNotice(timeline)
	if got != "Bati is building your battery history. Available data: 4h18m of 10d." {
		t.Fatalf("unexpected sparse data notice: %q", got)
	}
}

func TestPercentageAndDurationMapping(t *testing.T) {
	if got := percentageY(75, 200); got != 50 {
		t.Fatalf("75%% should map to y=50, got %v", got)
	}
	if got := percentageY(110, 200); got != 0 {
		t.Fatalf("percentage mapping must clamp high values, got %v", got)
	}
	if got := durationHeight(30*time.Minute, time.Hour, 120); got != 60 {
		t.Fatalf("30m of a 60m axis should map to 60px, got %v", got)
	}
	if got := durationHeight(2*time.Hour, time.Hour, 120); got != 120 {
		t.Fatalf("duration mapping must clamp to plot height, got %v", got)
	}
}
