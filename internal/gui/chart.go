package gui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"bati/internal/dto"
	"bati/internal/model"
	"bati/internal/statusfmt"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

type Viewport struct {
	Width, Height float32
	MinX, MaxX    float64
	MinY, MaxY    float64
}

type PlotBounds struct {
	Width, Height float32
}

func (bounds PlotBounds) ClampPoint(x, y float32) (float32, float32) {
	return min(max(x, 0), bounds.Width), min(max(y, 0), bounds.Height)
}

func TooltipBounds(anchorX, anchorY float32, plotWidth int, minY, maxY int, width, height int) image.Rectangle {
	return placeTooltip(
		image.Pt(int(math.Round(float64(anchorX))), int(math.Round(float64(anchorY)))),
		image.Rect(0, minY, max(plotWidth, 0), maxY),
		image.Pt(width, height),
		12,
	)
}

func (viewport Viewport) MapToScreen(x, y float64) (float32, float32) {
	var screenX, screenY float32
	if viewport.MaxX != viewport.MinX {
		xRatio := (x - viewport.MinX) / (viewport.MaxX - viewport.MinX)
		screenX = float32(xRatio) * viewport.Width
	}
	if viewport.MaxY != viewport.MinY {
		yRatio := (y - viewport.MinY) / (viewport.MaxY - viewport.MinY)
		screenY = viewport.Height - float32(yRatio)*viewport.Height
	}
	return screenX, screenY
}

func (viewport Viewport) MapToData(screenX, screenY float32) (float64, float64) {
	x, y := viewport.MinX, viewport.MinY
	if viewport.Width != 0 && viewport.MaxX != viewport.MinX {
		x = viewport.MinX + float64(screenX/viewport.Width)*(viewport.MaxX-viewport.MinX)
	}
	if viewport.Height != 0 && viewport.MaxY != viewport.MinY {
		y = viewport.MinY + float64((viewport.Height-screenY)/viewport.Height)*(viewport.MaxY-viewport.MinY)
	}
	return x, y
}

func TimestampAtX(start, end time.Time, width, x float32) time.Time {
	if width <= 0 || !end.After(start) {
		return start
	}
	ratio := min(1, max(0, float64(x/width)))
	return start.Add(time.Duration(float64(end.Sub(start)) * ratio))
}

func AxisLabels(start, end time.Time, use24h bool, count int) []string {
	ticks := axisTicks(start, end, use24h, count, time.Local)
	labels := make([]string, len(ticks))
	for index, tick := range ticks {
		labels[index] = tick.Label
	}
	return labels
}

func LookupClosestPoint(points []dto.TimelinePointDTO, viewport Viewport, mouseX, maxDistance float32) (int, float32, float32, bool) {
	if len(points) == 0 {
		return -1, 0, 0, false
	}
	targetUnix, _ := viewport.MapToData(mouseX, 0)
	target := time.Unix(int64(targetUnix), 0)
	closest := nearestPointIndex(points, target)
	screenX, screenY := viewport.MapToScreen(
		float64(points[closest].Timestamp.Unix()),
		plotBatteryCapacity(points[closest].Capacity),
	)
	if float32(math.Abs(float64(screenX-mouseX))) > maxDistance {
		return -1, 0, 0, false
	}
	return closest, screenX, screenY, true
}

func plotBatteryCapacity(value float64) float64 {
	return min(100, max(0, value))
}

func percentageY(value float64, height float32) float32 {
	return height - float32(plotBatteryCapacity(value)/100)*height
}

func durationHeight(value, maximum time.Duration, height float32) float32 {
	if value <= 0 || maximum <= 0 || height <= 0 {
		return 0
	}
	return min(height, float32(float64(value)/float64(maximum))*height)
}

func LookupSessionAtPosition(
	sessions []model.Session,
	viewport Viewport,
	mouseX, mouseY float32,
	markerTop float32,
) (int, bool) {
	timestamp := TimestampAtX(
		time.Unix(int64(viewport.MinX), 0),
		time.Unix(int64(viewport.MaxX), 0),
		viewport.Width,
		mouseX,
	)
	for index := len(sessions) - 1; index >= 0; index-- {
		session := sessions[index]
		if timestamp.Before(session.StartTime) || timestamp.After(session.EndTime) {
			continue
		}
		sessionType := normalizedSessionType(session.Type)
		if sessionType == "sleeping" {
			return index, true
		}
		if isStateMarker(sessionType) && mouseY >= markerTop {
			return index, true
		}
	}
	return -1, false
}

func LookupSessionAtTime(sessions []model.Session, timestamp time.Time) int {
	for index := len(sessions) - 1; index >= 0; index-- {
		session := sessions[index]
		if !timestamp.Before(session.StartTime) && !timestamp.After(session.EndTime) {
			return index
		}
	}
	return -1
}

func isStateMarker(sessionType string) bool {
	return sessionType == "charging" ||
		sessionType == "discharging" ||
		sessionType == "full" ||
		sessionType == "not_charging"
}

func SparseDataNotice(timeline dto.TimelineDTO) string {
	selectedDuration := timeline.End.Sub(timeline.Start)
	availableDuration := timeline.AvailableTo.Sub(timeline.AvailableFrom)
	if selectedDuration <= 0 || availableDuration <= 0 || availableDuration >= selectedDuration*3/4 {
		return ""
	}
	return fmt.Sprintf(
		"Bati is building your battery history. Available data: %s of %s.",
		formatDuration(availableDuration),
		formatRangeDuration(selectedDuration),
	)
}

func ChartDisplayRange(timeline dto.TimelineDTO) (time.Time, time.Time) {
	selectedDuration := timeline.End.Sub(timeline.Start)
	availableDuration := timeline.AvailableTo.Sub(timeline.AvailableFrom)
	if selectedDuration <= 0 || availableDuration <= 0 || availableDuration >= selectedDuration*3/4 {
		return timeline.Start, timeline.End
	}

	padding := min(max(availableDuration/16, 10*time.Minute), time.Hour)
	start := maxTime(timeline.Start, timeline.AvailableFrom.Add(-padding))
	end := minTime(timeline.End, timeline.AvailableTo.Add(padding))
	return start, end
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

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

type ChartDataState uint8

const (
	ChartEmpty ChartDataState = iota
	ChartInsufficient
	ChartReady
)

func TimelineRenderState(points []dto.TimelinePointDTO) ChartDataState {
	switch len(points) {
	case 0:
		return ChartEmpty
	case 1:
		return ChartInsufficient
	default:
		return ChartReady
	}
}

func ContinuousSegments(points []dto.TimelinePointDTO, maximumGap time.Duration) [][]dto.TimelinePointDTO {
	if len(points) == 0 {
		return nil
	}
	segments := make([][]dto.TimelinePointDTO, 0, 1)
	start := 0
	for index := 1; index < len(points); index++ {
		if points[index].Timestamp.Sub(points[index-1].Timestamp) > maximumGap {
			if index-start >= 2 {
				segments = append(segments, points[start:index])
			}
			start = index
		}
	}
	if len(points)-start >= 2 {
		segments = append(segments, points[start:])
	}
	return segments
}

func (state *UIState) layoutBatteryChart(gtx layout.Context) layout.Dimensions {
	return state.card(gtx, 382, func(gtx layout.Context) layout.Dimensions {
		graphHeight := gtx.Dp(142)
		markerTop := gtx.Dp(174)
		plotHeight := gtx.Dp(214)
		plotWidth := gtx.Constraints.Max.X - gtx.Dp(88)
		offset := image.Pt(gtx.Dp(20), gtx.Dp(112))

		titleOffset := op.Offset(image.Pt(gtx.Dp(20), gtx.Dp(18))).Push(gtx.Ops)
		sectionTitle(state.theme, "Battery Level").Layout(gtx)
		titleOffset.Pop()
		subtitleOffset := op.Offset(image.Pt(gtx.Dp(20), gtx.Dp(43))).Push(gtx.Ops)
		subtitle := material.Caption(state.theme, "historical telemetry only")
		subtitle.Color = palette.muted
		subtitle.Layout(gtx)
		subtitleOffset.Pop()

		state.drawBatteryLegend(gtx, plotWidth)
		state.drawCurrentBatteryCallout(gtx, plotWidth)

		points := state.data.Timeline.Points
		state.drawRangeSummary(gtx)
		if state.history.RenderState == ChartEmpty {
			return state.chartEmptyState(gtx, "No battery samples in the selected range")
		}

		viewport := Viewport{
			Width:  float32(plotWidth),
			Height: float32(graphHeight),
			MinX:   float64(state.history.DisplayStart.Unix()),
			MaxX:   float64(state.history.DisplayEnd.Unix()),
			MinY:   -2,
			MaxY:   102,
		}

		plotOffset := op.Offset(offset).Push(gtx.Ops)
		plotClip := clip.Rect{Max: image.Pt(plotWidth, plotHeight)}.Push(gtx.Ops)
		state.handleBatteryPointer(gtx, plotWidth, plotHeight)
		if state.batteryHovered {
			state.updateHoverContext(state.batteryMouseX, state.batteryMouseY, true, viewport, float32(markerTop))
		}
		drawAvailableRange(gtx, viewport, state.data.Timeline)
		drawGapRegions(gtx, viewport, state.history.Gaps, graphHeight, plotWidth)
		drawChartGrid(gtx, viewport, plotWidth, graphHeight, state.history.BatteryTicks)
		drawSessionRegions(gtx, viewport, state.data.Timeline.Sessions)
		state.drawChargeLimitLine(gtx, viewport, state.history.ChargeLimit, plotWidth)
		for _, segment := range state.history.Segments {
			drawBatteryLine(gtx, viewport, segment)
		}
		if len(points) == 1 {
			x, y := viewport.MapToScreen(
				float64(points[0].Timestamp.Unix()),
				plotBatteryCapacity(points[0].Capacity),
			)
			drawBatteryPoint(gtx, x, y)
		}
		state.drawCurrentBatteryMarker(gtx, viewport, state.history.Current, plotWidth)
		drawSessionRow(gtx, viewport, state.data.Timeline.Sessions, markerTop, plotHeight)

		var tooltipToDraw func()
		if state.hoverContext.Active {
			hoverX, _ := viewport.MapToScreen(float64(state.hoverContext.HoverTime.Unix()), 0)
			drawLine(gtx, hoverX, 0, hoverX, float32(plotHeight), palette.hoverGuide, 1)

			if state.hoverContext.NearestBatterySample != nil {
				dotX, dotY := viewport.MapToScreen(
					float64(state.hoverContext.NearestBatterySample.Timestamp.Unix()),
					plotBatteryCapacity(state.hoverContext.NearestBatterySample.Capacity),
				)
				drawBatteryPoint(gtx, dotX, dotY)
			}

			if state.batteryHovered {
				if state.hoverContext.CurrentMarkerHit {
					tooltipToDraw = func() {
						state.drawTooltipModel(
							gtx,
							currentBatteryTooltip(state.history.Current),
							hoverX,
							percentageY(state.history.Current.CapacityPercent, float32(graphHeight)),
							plotWidth,
							gtx.Dp(-112),
							gtx.Dp(284),
						)
					}
				} else if state.hoverContext.ChargeLimitHit {
					tooltipToDraw = func() {
						_, limitY := viewport.MapToScreen(viewport.MinX, float64(state.history.ChargeLimit.Percent))
						state.drawTooltipModel(
							gtx,
							chargeLimitTooltip(state.history.ChargeLimit, state.data.LiveSnapshot),
							hoverX,
							limitY,
							plotWidth,
							gtx.Dp(-112),
							gtx.Dp(284),
						)
					}
				} else if state.hoverContext.GapMarker != nil {
					gap := *state.hoverContext.GapMarker
					tooltipToDraw = func() {
						state.drawTooltipModel(
							gtx,
							gapTooltip(gap, state.location),
							hoverX,
							float32(markerTop),
							plotWidth,
							gtx.Dp(-112),
							gtx.Dp(284),
						)
					}
				} else if state.hoverContext.StateStripHit && state.hoverContext.ContainingBatterySession != nil {
					tooltipToDraw = func() {
						state.drawTooltipModel(
							gtx,
							sessionTooltip(*state.hoverContext.ContainingBatterySession, state.hoverContext.HoverTime, state.location),
							hoverX,
							state.batteryMouseY,
							plotWidth,
							gtx.Dp(-112),
							gtx.Dp(284),
						)
					}
				} else if state.hoverContext.BatteryPlotHit && state.hoverContext.NearestBatterySample != nil {
					point := *state.hoverContext.NearestBatterySample
					tooltipToDraw = func() {
						state.drawTooltipModel(
							gtx,
							pointTooltip(point, state.hoverContext.ContainingBatterySession, state.hoverContext.HoverTime, state.location),
							hoverX,
							percentageY(point.Capacity, float32(graphHeight)),
							plotWidth,
							gtx.Dp(-112),
							gtx.Dp(284),
						)
					}
				}
			}
		}
		plotClip.Pop()
		if tooltipToDraw != nil {
			tooltipToDraw()
		}
		plotOffset.Pop()

		state.drawYAxisLabels(gtx, offset.X+plotWidth+gtx.Dp(10), offset.Y, viewport)
		state.drawXAxisTicks(gtx, offset.X, offset.Y+plotHeight+gtx.Dp(8), viewport, state.history.BatteryTicks)
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(382))}
	})
}

func (state *UIState) handleBatteryPointer(gtx layout.Context, width, height int) {
	area := clip.Rect{Max: image.Pt(width, height)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &state.batteryChartTarget)
	area.Pop()
	for {
		raw, ok := gtx.Event(pointer.Filter{Target: &state.batteryChartTarget, Kinds: pointer.Move | pointer.Enter | pointer.Leave})
		if !ok {
			return
		}
		pointerEvent, ok := raw.(pointer.Event)
		if !ok {
			continue
		}
		if pointerEvent.Kind == pointer.Leave {
			state.clearHover()
			continue
		}
		state.batteryHovered = true
		state.batteryMouseX = pointerEvent.Position.X
		state.batteryMouseY = pointerEvent.Position.Y
	}
}

func drawAvailableRange(gtx layout.Context, viewport Viewport, timeline dto.TimelineDTO) {
	if timeline.AvailableFrom.IsZero() || timeline.AvailableTo.IsZero() {
		return
	}
	startX, _ := viewport.MapToScreen(float64(timeline.AvailableFrom.Unix()), 0)
	endX, _ := viewport.MapToScreen(float64(timeline.AvailableTo.Unix()), 0)
	startX = max(0, startX)
	endX = min(viewport.Width, endX)
	if endX <= startX {
		return
	}
	fillRect(gtx, image.Rect(int(startX), 0, int(endX), int(viewport.Height)), palette.availableRange)
}

func drawChartGrid(gtx layout.Context, viewport Viewport, width, height int, ticks []axisTick) {
	for _, value := range []float64{0, 25, 50, 75, 100} {
		_, y := viewport.MapToScreen(viewport.MinX, value)
		drawLine(gtx, 0, y, float32(width), y, palette.border, 1)
	}
	for _, tick := range ticks {
		x, _ := viewport.MapToScreen(float64(tick.Time.Unix()), 0)
		drawLine(gtx, x, 0, x, float32(height), palette.border, 1)
	}
}

func (state *UIState) drawCurrentBatteryCallout(gtx layout.Context, plotWidth int) {
	if !state.history.Current.Available {
		return
	}
	text := fmt.Sprintf(
		"current: %.0f%% · %s",
		state.history.Current.CapacityPercent,
		statusfmt.Lower(state.history.Current.Status),
	)
	x := max(gtx.Dp(20), plotWidth-gtx.Dp(220))
	offset := op.Offset(image.Pt(x, gtx.Dp(43))).Push(gtx.Ops)
	layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			fillRoundedRect(gtx, image.Rect(0, gtx.Dp(4), gtx.Dp(8), gtx.Dp(12)), gtx.Dp(4), palette.green)
			return layout.Dimensions{Size: image.Pt(gtx.Dp(14), gtx.Dp(14))}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			label := material.Caption(state.theme, text)
			label.Color = palette.muted
			label.Font.Weight = font.Medium
			return label.Layout(gtx)
		}),
	)
	offset.Pop()
}

func (state *UIState) drawBatteryLegend(gtx layout.Context, plotWidth int) {
	limitLabel := "charge limit"
	if state.history.ChargeLimit.Available {
		limitLabel = state.history.ChargeLimit.Label
	}
	legendOffset := op.Offset(image.Pt(max(gtx.Dp(280), plotWidth-gtx.Dp(330)), gtx.Dp(18))).Push(gtx.Ops)
	layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			fillRect(gtx, image.Rect(0, gtx.Dp(4), gtx.Dp(12), gtx.Dp(8)), palette.green)
			return layout.Dimensions{Size: image.Pt(gtx.Dp(16), gtx.Dp(12))}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(state.theme, "level")
			lbl.Color = palette.muted
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			drawDashedLine(gtx, 0, float32(gtx.Dp(6)), float32(gtx.Dp(16)), float32(gtx.Dp(6)), palette.threshold, 1.5, 4, 3)
			return layout.Dimensions{Size: image.Pt(gtx.Dp(20), gtx.Dp(12))}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(state.theme, limitLabel)
			lbl.Color = palette.muted
			return lbl.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			fillRoundedRect(gtx, image.Rect(0, gtx.Dp(5), gtx.Dp(14), gtx.Dp(9)), gtx.Dp(2), palette.fullMarker)
			return layout.Dimensions{Size: image.Pt(gtx.Dp(18), gtx.Dp(12))}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(state.theme, "sessions")
			lbl.Color = palette.muted
			return lbl.Layout(gtx)
		}),
	)
	legendOffset.Pop()
}

func drawSessionRegions(gtx layout.Context, viewport Viewport, sessions []model.Session) {
	for _, session := range sessions {
		if normalizedSessionType(session.Type) != "sleeping" {
			continue
		}
		startX, _ := viewport.MapToScreen(float64(session.StartTime.Unix()), 0)
		endX, _ := viewport.MapToScreen(float64(session.EndTime.Unix()), 0)
		fillRect(gtx, image.Rect(int(startX), 0, int(endX), int(viewport.Height)), palette.sleep)
	}
}

func (state *UIState) drawChargeLimitLine(gtx layout.Context, viewport Viewport, limit chargeLimitLineModel, width int) {
	if !limit.Available {
		return
	}
	_, y := viewport.MapToScreen(viewport.MinX, float64(limit.Percent))
	if y < 0 || y > viewport.Height {
		return
	}
	drawDashedLine(gtx, 0, y, float32(width), y, palette.threshold, 1.5, 8, 6)
}

func drawBatteryLine(gtx layout.Context, viewport Viewport, points []dto.TimelinePointDTO) {
	startX, startY := viewport.MapToScreen(
		float64(points[0].Timestamp.Unix()),
		plotBatteryCapacity(points[0].Capacity),
	)
	paint.ColorOp{Color: palette.green}.Add(gtx.Ops)
	var line clip.Path
	line.Begin(gtx.Ops)
	line.MoveTo(f32.Pt(startX, startY))
	for _, point := range points[1:] {
		x, y := viewport.MapToScreen(
			float64(point.Timestamp.Unix()),
			plotBatteryCapacity(point.Capacity),
		)
		line.LineTo(f32.Pt(x, startY))
		line.LineTo(f32.Pt(x, y))
		startY = y
	}
	stack := clip.Stroke{Path: line.End(), Width: 3.5}.Op().Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
}

func (state *UIState) drawCurrentBatteryMarker(
	gtx layout.Context,
	viewport Viewport,
	marker currentBatteryMarkerModel,
	width int,
) {
	if !marker.Available {
		return
	}
	x := float32(max(0, width-18))
	y := percentageY(marker.CapacityPercent, viewport.Height)
	if y < 0 || y > viewport.Height {
		return
	}
	markerHalfHeight := float32(gtx.Dp(12))
	drawLine(gtx, x, y-markerHalfHeight, x, y+markerHalfHeight, palette.greenFill, 3)
	drawBatteryRing(gtx, x, y)
}

func drawGapRegions(
	gtx layout.Context,
	viewport Viewport,
	gaps []historyGapMarkerModel,
	graphHeight int,
	width int,
) {
	for _, gap := range gaps {
		left, right, visible := gapRegionBounds(gap, viewport, width)
		if !visible {
			continue
		}
		fillRect(gtx, image.Rect(int(left), 0, int(right), graphHeight), palette.gapRegion)
	}
}

func drawSessionRow(
	gtx layout.Context,
	viewport Viewport,
	sessions []model.Session,
	top, height int,
) {
	center := float32(top + (height-top)/2)
	drawLine(gtx, 0, center, viewport.Width, center, palette.border, 1)
	for _, session := range sessions {
		sessionType := normalizedSessionType(session.Type)
		if !isStateMarker(sessionType) {
			continue
		}
		startX, _ := viewport.MapToScreen(float64(session.StartTime.Unix()), 0)
		endX, _ := viewport.MapToScreen(float64(session.EndTime.Unix()), 0)
		startX = max(0, startX)
		endX = min(viewport.Width, endX)
		if endX <= startX {
			continue
		}
		markerColor := sessionRowColor(sessionType)
		fillRoundedRect(gtx, image.Rect(int(startX), int(center)-3, int(endX), int(center)+3), 3, markerColor)
	}
}

func sessionRowColor(sessionType string) color.NRGBA {
	switch sessionType {
	case "full":
		return palette.fullMarker
	case "discharging":
		return color.NRGBA{R: palette.discharging.R, G: palette.discharging.G, B: palette.discharging.B, A: 190}
	case "not_charging":
		return palette.threshold
	case "charging":
		return color.NRGBA{R: 82, G: 137, B: 105, A: 190}
	default:
		return palette.muted
	}
}

func normalizedSessionType(value string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), " ", "_")
}

func drawCursor(gtx layout.Context, x, y float32, width, graphHeight, guideHeight int) {
	x, y = (PlotBounds{Width: float32(width), Height: float32(graphHeight)}).ClampPoint(x, y)
	drawLine(gtx, x, 0, x, float32(guideHeight), palette.muted, 1)
	drawBatteryPoint(gtx, x, y)
}

func drawBatteryPoint(gtx layout.Context, x, y float32) {
	dot := clip.Ellipse(image.Rect(int(x)-4, int(y)-4, int(x)+4, int(y)+4))
	paint.ColorOp{Color: palette.green}.Add(gtx.Ops)
	stack := dot.Op(gtx.Ops).Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
}

func drawBatteryRing(gtx layout.Context, x, y float32) {
	outer := clip.Ellipse(image.Rect(int(x)-6, int(y)-6, int(x)+6, int(y)+6))
	paint.ColorOp{Color: palette.green}.Add(gtx.Ops)
	outerStack := outer.Op(gtx.Ops).Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	outerStack.Pop()
	inner := clip.Ellipse(image.Rect(int(x)-3, int(y)-3, int(x)+3, int(y)+3))
	paint.ColorOp{Color: palette.card}.Add(gtx.Ops)
	innerStack := inner.Op(gtx.Ops).Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	innerStack.Pop()
}

func (state *UIState) drawTooltipModel(
	gtx layout.Context,
	model tooltipModel,
	x, y float32,
	plotWidth int,
	minY, maxY int,
) {
	if model.Title == "" {
		return
	}
	requestedWidth := gtx.Dp(236)
	requestedHeight := gtx.Dp(unit.Dp(28 + len(model.Rows)*20))
	bounds := TooltipBounds(x, y, plotWidth, minY, maxY, requestedWidth, requestedHeight)
	width, height := bounds.Dx(), bounds.Dy()
	offset := op.Offset(bounds.Min).Push(gtx.Ops)
	drawRoundedRect(gtx, image.Pt(width, height), 12, palette.tooltip, palette.border)
	layout.Inset{Top: unit.Dp(9), Left: unit.Dp(11), Right: unit.Dp(11)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := make([]layout.FlexChild, 0, len(model.Rows)+1)
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			label := material.Body2(state.theme, model.Title)
			label.Color = palette.text
			label.Font.Weight = font.SemiBold
			return label.Layout(gtx)
		}))
		for _, row := range model.Rows {
			row := row
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body2(state.theme, row)
				label.Color = palette.muted
				return label.Layout(gtx)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	offset.Pop()
}

func (state *UIState) layoutScreenChart(gtx layout.Context) layout.Dimensions {
	return state.card(gtx, 252, func(gtx layout.Context) layout.Dimensions {
		titleOffset := op.Offset(image.Pt(gtx.Dp(20), gtx.Dp(18))).Push(gtx.Ops)
		sectionTitle(state.theme, "Screen Activity").Layout(gtx)
		titleOffset.Pop()

		bars := state.data.Timeline.SOTBars
		if !state.history.HasCoverage {
			return state.chartEmptyState(gtx, "No observed screen activity intervals in this range")
		}

		plotWidth := gtx.Constraints.Max.X - gtx.Dp(88)
		plotHeight := gtx.Dp(124)
		viewport := Viewport{
			Width:  float32(plotWidth),
			Height: float32(plotHeight),
			MinX:   float64(state.history.DisplayStart.Unix()),
			MaxX:   float64(state.history.DisplayEnd.Unix()),
		}
		if !state.use24h {
			viewport.MinX = float64(state.data.Timeline.Start.Unix())
			viewport.MaxX = float64(state.data.Timeline.End.Unix())
		}
		offset := op.Offset(image.Pt(gtx.Dp(20), gtx.Dp(58))).Push(gtx.Ops)
		plotClip := clip.Rect{Max: image.Pt(plotWidth, plotHeight)}.Push(gtx.Ops)
		state.handleScreenPointer(gtx, plotWidth, plotHeight)
		if state.screenHovered {
			state.updateHoverContext(state.screenMouseX, 0, false, viewport, 0)
		}
		for index := 0; index <= 2; index++ {
			y := float32(plotHeight) * float32(index) / 2
			drawLine(gtx, 0, y, float32(plotWidth), y, palette.border, 1)
		}
		axisMaximum := state.history.MaxScreen
		gap := float32(gtx.Dp(2))
		for _, bar := range bars {
			if !bar.Observed || bar.Duration <= 0 {
				continue
			}
			left, right, visible := BarBounds(viewport, bar, gap)
			if !visible {
				continue
			}
			height := durationHeight(bar.Duration, axisMaximum, float32(plotHeight))
			if height > 0 {
				fillRoundedRect(gtx, image.Rect(int(left), plotHeight-int(height), int(right), plotHeight), 2, palette.blue)
			}
		}
		var tooltipToDraw func()
		if state.hoverContext.Active {
			hoverX, _ := viewport.MapToScreen(float64(state.hoverContext.HoverTime.Unix()), 0)
			drawLine(gtx, hoverX, 0, hoverX, float32(plotHeight), palette.hoverGuide, 1)

			if state.screenHovered && state.hoverContext.ScreenActivityBin != nil && state.hoverContext.ScreenActivityBin.Observed {
				bar := *state.hoverContext.ScreenActivityBin
				left, right, visible := BarBounds(viewport, bar, gap)
				if visible {
					fillRect(gtx, image.Rect(int(left), 0, int(right), plotHeight), palette.blueHighlight)
				}
				var day *dto.DailySummaryDTO
				if !state.use24h {
					for i := range state.data.Timeline.Days {
						if state.data.Timeline.Days[i].Start.Equal(bar.Start) {
							daily := state.data.Timeline.Days[i]
							day = &daily
							break
						}
					}
				}
				tooltipToDraw = func() {
					state.drawTooltipModel(
						gtx,
						screenTooltip(bar, day, state.location),
						hoverX,
						float32(plotHeight)/2,
						plotWidth,
						gtx.Dp(-58),
						gtx.Dp(194),
					)
				}
			}
		}
		plotClip.Pop()
		if tooltipToDraw != nil {
			tooltipToDraw()
		}
		offset.Pop()
		state.drawScreenYAxisLabels(gtx, gtx.Dp(20)+plotWidth+gtx.Dp(10), gtx.Dp(58), plotHeight, axisMaximum)
		state.drawXAxisTicks(
			gtx,
			gtx.Dp(20),
			gtx.Dp(58)+plotHeight+gtx.Dp(8),
			viewport,
			state.history.ScreenTicks,
		)
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(252))}
	})
}

func screenAxisMaximum(maximum time.Duration) time.Duration {
	const step = 30 * time.Minute
	if maximum <= 0 {
		return step
	}
	steps := (maximum + step - 1) / step
	if maximum%step == 0 {
		steps++
	}
	return steps * step
}

func BarBounds(viewport Viewport, bar dto.SOTBarDTO, gap float32) (float32, float32, bool) {
	if viewport.Width <= 0 || !bar.End.After(bar.Start) {
		return 0, 0, false
	}
	left, _ := viewport.MapToScreen(float64(bar.Start.Unix()), 0)
	right, _ := viewport.MapToScreen(float64(bar.End.Unix()), 0)
	left = max(0, left)
	right = min(viewport.Width, right)
	if right <= left {
		return 0, 0, false
	}
	inset := min(gap/2, (right-left)/4)
	return left + inset, right - inset, true
}

func LookupBarAtX(
	bars []dto.SOTBarDTO,
	viewport Viewport,
	gap, mouseX float32,
) (int, float32, float32, bool) {
	if len(bars) == 0 || viewport.Width <= 0 {
		return -1, 0, 0, false
	}
	for index, bar := range bars {
		left, right, visible := BarBounds(viewport, bar, gap)
		if !visible {
			continue
		}
		if mouseX >= left && mouseX <= right {
			return index, left, right, true
		}
	}
	return -1, 0, 0, false
}

func (state *UIState) handleScreenPointer(gtx layout.Context, width, height int) {
	area := clip.Rect{Max: image.Pt(width, height)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &state.screenChartTarget)
	area.Pop()
	for {
		raw, ok := gtx.Event(pointer.Filter{Target: &state.screenChartTarget, Kinds: pointer.Move | pointer.Enter | pointer.Leave})
		if !ok {
			return
		}
		pointerEvent, ok := raw.(pointer.Event)
		if !ok {
			continue
		}
		if pointerEvent.Kind == pointer.Leave {
			state.clearHover()
			continue
		}
		state.screenHovered = true
		state.screenMouseX = pointerEvent.Position.X
	}
}

func (state *UIState) chartEmptyState(gtx layout.Context, message string) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return secondary(state.theme, message).Layout(gtx)
	})
}

func (state *UIState) drawYAxisLabels(gtx layout.Context, x, y int, viewport Viewport) {
	for _, value := range []float64{100, 50, 0} {
		_, screenY := viewport.MapToScreen(viewport.MinX, value)
		top := y + int(screenY) - gtx.Dp(7)
		offset := op.Offset(image.Pt(x, top)).Push(gtx.Ops)
		label := material.Caption(state.theme, fmt.Sprintf("%.0f%%", value))
		label.Color = palette.muted
		label.Layout(gtx)
		offset.Pop()
	}
}

func (state *UIState) drawXAxisTicks(
	gtx layout.Context,
	x, y int,
	viewport Viewport,
	ticks []axisTick,
) {
	for index, tick := range ticks {
		position, _ := viewport.MapToScreen(float64(tick.Time.Unix()), 0)
		left := x + int(position)
		if index > 0 {
			left -= gtx.Dp(18)
		}
		offset := op.Offset(image.Pt(left, y)).Push(gtx.Ops)
		label := material.Caption(state.theme, tick.Label)
		label.Color = palette.muted
		label.Layout(gtx)
		offset.Pop()
	}
}

func (state *UIState) drawScreenYAxisLabels(gtx layout.Context, x, y, height int, maximum time.Duration) {
	labels := []string{
		formatAxisDuration(maximum),
		formatAxisDuration(maximum / 2),
		"0m",
	}
	for index, text := range labels {
		top := y + height*index/2 - gtx.Dp(7)
		offset := op.Offset(image.Pt(x, top)).Push(gtx.Ops)
		label := material.Caption(state.theme, text)
		label.Color = palette.muted
		label.Layout(gtx)
		offset.Pop()
	}
}

func availableRangeText(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "Available data range: Not available"
	}
	return fmt.Sprintf(
		"Available data range: %s - %s",
		start.Local().Format("02 Jan 15:04"),
		end.Local().Format("02 Jan 15:04"),
	)
}

func selectedRangeText(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "Selected range: Not available"
	}
	return fmt.Sprintf(
		"Selected range: %s - %s",
		start.Local().Format("02 Jan 15:04"),
		end.Local().Format("02 Jan 15:04"),
	)
}

func (state *UIState) drawRangeSummary(gtx layout.Context) {
	selectedOffset := op.Offset(image.Pt(gtx.Dp(20), gtx.Dp(59))).Push(gtx.Ops)
	secondary(state.theme, selectedRangeText(state.data.Timeline.Start, state.data.Timeline.End)).Layout(gtx)
	selectedOffset.Pop()
	availableOffset := op.Offset(image.Pt(gtx.Dp(20), gtx.Dp(77))).Push(gtx.Ops)
	secondary(state.theme, availableRangeText(state.data.Timeline.AvailableFrom, state.data.Timeline.AvailableTo)).Layout(gtx)
	availableOffset.Pop()
}

func drawLine(gtx layout.Context, x1, y1, x2, y2 float32, colour color.NRGBA, width float32) {
	paint.ColorOp{Color: colour}.Add(gtx.Ops)
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(x1, y1))
	path.LineTo(f32.Pt(x2, y2))
	stack := clip.Stroke{Path: path.End(), Width: width}.Op().Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
}

func drawDashedLine(gtx layout.Context, x1, y1, x2, y2 float32, colour color.NRGBA, width, dash, gap float32) {
	if dash <= 0 || gap < 0 {
		drawLine(gtx, x1, y1, x2, y2, colour, width)
		return
	}
	dx := x2 - x1
	dy := y2 - y1
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length <= 0 {
		return
	}
	stepX := dx / length
	stepY := dy / length
	for offset := float32(0); offset < length; offset += dash + gap {
		end := min(offset+dash, length)
		drawLine(
			gtx,
			x1+stepX*offset,
			y1+stepY*offset,
			x1+stepX*end,
			y1+stepY*end,
			colour,
			width,
		)
	}
}

func fillRect(gtx layout.Context, rectangle image.Rectangle, colour color.NRGBA) {
	paint.FillShape(gtx.Ops, colour, clip.Rect(rectangle).Op())
}

func fillRoundedRect(gtx layout.Context, rectangle image.Rectangle, radius int, colour color.NRGBA) {
	paint.FillShape(gtx.Ops, colour, clip.UniformRRect(rectangle, radius).Op(gtx.Ops))
}

func (state *UIState) updateHoverContext(mouseX, mouseY float32, isBatteryChart bool, viewport Viewport, markerTop float32) {
	if !state.hasData() {
		return
	}
	targetUnix, _ := viewport.MapToData(mouseX, 0)
	hoverTime := time.Unix(int64(targetUnix), 0)

	state.hoverContext.Active = true
	state.hoverContext.HoverTime = hoverTime
	state.hoverContext.ChargeLimitHit = false
	state.hoverContext.CurrentMarkerHit = false
	state.hoverContext.GapMarker = nil

	if isBatteryChart {
		state.hoverContext.ScreenChartHit = false
		if currentMarkerHit(mouseX, mouseY, viewport, state.history.Current) {
			state.hoverContext.CurrentMarkerHit = true
			state.hoverContext.StateStripHit = false
			state.hoverContext.BatteryPlotHit = false
		} else if chargeLimitHit(mouseY, viewport, state.history.ChargeLimit) {
			state.hoverContext.ChargeLimitHit = true
			state.hoverContext.StateStripHit = false
			state.hoverContext.BatteryPlotHit = false
		} else if gapIndex := lookupGapMarkerAtX(state.history.Gaps, viewport, mouseX, mouseY, markerTop); gapIndex >= 0 {
			state.hoverContext.GapMarker = &state.history.Gaps[gapIndex]
			state.hoverContext.StateStripHit = false
			state.hoverContext.BatteryPlotHit = false
		} else if mouseY >= markerTop {
			state.hoverContext.StateStripHit = true
			state.hoverContext.BatteryPlotHit = false
		} else {
			state.hoverContext.StateStripHit = false
			state.hoverContext.BatteryPlotHit = true
		}
	} else {
		state.hoverContext.StateStripHit = false
		state.hoverContext.BatteryPlotHit = false
		state.hoverContext.ScreenChartHit = true
	}

	// 1. Find nearest battery sample
	points := state.data.Timeline.Points
	if len(points) > 0 {
		closestIdx := nearestPointIndex(points, hoverTime)
		state.hoverContext.NearestBatterySample = &points[closestIdx]
	} else {
		state.hoverContext.NearestBatterySample = nil
	}

	// 2. Find containing battery session
	sessionIdx := LookupSessionAtTime(state.data.Timeline.Sessions, hoverTime)
	if sessionIdx >= 0 {
		state.hoverContext.ContainingBatterySession = &state.data.Timeline.Sessions[sessionIdx]
	} else {
		state.hoverContext.ContainingBatterySession = nil
	}

	// 3. Find screen activity bin containing hoverTime
	bars := state.data.Timeline.SOTBars
	state.hoverContext.ScreenActivityBin = nil
	for i := range bars {
		if !hoverTime.Before(bars[i].Start) && hoverTime.Before(bars[i].End) {
			state.hoverContext.ScreenActivityBin = &bars[i]
			break
		}
	}
}

func currentMarkerHit(mouseX, mouseY float32, viewport Viewport, marker currentBatteryMarkerModel) bool {
	if !marker.Available || viewport.Width <= 0 {
		return false
	}
	x := viewport.Width - 18
	y := percentageY(marker.CapacityPercent, viewport.Height)
	return math.Abs(float64(mouseX-x)) <= 14 && math.Abs(float64(mouseY-y)) <= 14
}

func chargeLimitHit(mouseY float32, viewport Viewport, limit chargeLimitLineModel) bool {
	if !limit.Available {
		return false
	}
	_, y := viewport.MapToScreen(viewport.MinX, float64(limit.Percent))
	return math.Abs(float64(mouseY-y)) <= 7
}

func lookupGapMarkerAtX(gaps []historyGapMarkerModel, viewport Viewport, mouseX, mouseY, markerTop float32) int {
	if mouseY < 0 || mouseY > markerTop+24 {
		return -1
	}
	for index := len(gaps) - 1; index >= 0; index-- {
		left, right, visible := gapRegionBounds(gaps[index], viewport, int(viewport.Width))
		if !visible {
			continue
		}
		if mouseX >= left-6 && mouseX <= right+6 {
			return index
		}
	}
	return -1
}

func gapRegionBounds(gap historyGapMarkerModel, viewport Viewport, width int) (float32, float32, bool) {
	if width <= 0 || !gap.End.After(gap.Start) {
		return 0, 0, false
	}
	left, _ := viewport.MapToScreen(float64(gap.Start.Unix()), 0)
	right, _ := viewport.MapToScreen(float64(gap.End.Unix()), 0)
	if gap.Stale && (right < 0 || left > float32(width)) {
		return float32(max(0, width-42)), float32(width), true
	}
	left = max(0, left)
	right = min(float32(width), right)
	if right <= left {
		if gap.Stale {
			return float32(max(0, width-42)), float32(width), true
		}
		return 0, 0, false
	}
	if right-left < 8 {
		if !gap.Stale {
			return 0, 0, false
		}
		center := (left + right) / 2
		left = max(0, center-10)
		right = min(float32(width), center+10)
	}
	return left, right, true
}
