package gui

import (
	"fmt"
	"log"
	"sync"
	"time"

	"bati/internal/analytics"
	"bati/internal/csvexport"
	"bati/internal/db"
	"bati/internal/dto"
	"bati/internal/model"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type viewID uint8

const (
	viewOverview viewID = iota
	viewHistory
	viewHealth
	viewAbout
)

type UIState struct {
	theme    *material.Theme
	load     func(time.Time, bool) (*dto.DashboardDTO, error)
	data     *dto.DashboardDTO
	history  historyChartModel
	loadErr  error
	loading  bool
	results  chan refreshResult
	workers  sync.WaitGroup
	now      func() time.Time
	liveLoad func(string) (dto.LiveSnapshotDTO, error)
	location *time.Location

	view        viewID
	use24h      bool
	lastUpdated time.Time

	overviewButton widget.Clickable
	historyButton  widget.Clickable
	healthButton   widget.Clickable
	aboutButton    widget.Clickable
	hoursButton    widget.Clickable
	daysButton     widget.Clickable
	exportButton   widget.Clickable
	contentList    widget.List

	batteryChartTarget struct{ token byte }
	batteryHovered     bool
	batteryMouseX      float32
	batteryMouseY      float32
	hoveredPoint       *dto.TimelinePointDTO
	hoveredSession     *model.Session

	screenChartTarget struct{ token byte }
	screenHovered     bool
	screenMouseX      float32
	hoveredBar        *dto.SOTBarDTO

	hoverContext HoverContext

	exportMessage string
	exportErr     bool
	dbPath        string
}

type HoverContext struct {
	Active                   bool
	HoverTime                time.Time
	NearestBatterySample     *dto.TimelinePointDTO
	ContainingBatterySession *model.Session
	ScreenActivityBin        *dto.SOTBarDTO
	GapMarker                *historyGapMarkerModel
	StateStripHit            bool
	BatteryPlotHit           bool
	ScreenChartHit           bool
	ChargeLimitHit           bool
	CurrentMarkerHit         bool
}

type refreshResult struct {
	data   *dto.DashboardDTO
	err    error
	use24h bool
	at     time.Time
}

func Run(database *db.DB, dbPath string) error {
	window := new(app.Window)
	window.Option(app.Title("BATI"), app.Size(unit.Dp(1180), unit.Dp(780)))

	state := newUIState(func(now time.Time, use24h bool) (*dto.DashboardDTO, error) {
		return analytics.FetchDashboardData(database, now, use24h)
	})
	state.liveLoad = analytics.FetchLiveSnapshot
	state.dbPath = dbPath
	state.requestRefresh(window)

	refreshTick := make(chan struct{}, 1)
	liveTick := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		refreshTicker := time.NewTicker(time.Minute)
		defer refreshTicker.Stop()
		liveTicker := time.NewTicker(5 * time.Second)
		defer liveTicker.Stop()
		for {
			select {
			case <-refreshTicker.C:
				select {
				case refreshTick <- struct{}{}:
					window.Invalidate()
				default:
				}
			case <-liveTicker.C:
				select {
				case liveTick <- struct{}{}:
					window.Invalidate()
				default:
				}
			case <-done:
				return
			}
		}
	}()
	defer func() {
		close(done)
		state.workers.Wait()
	}()

	var operations op.Ops
	for {
		switch windowEvent := window.Event().(type) {
		case app.DestroyEvent:
			return windowEvent.Err
		case app.FrameEvent:
			gtx := app.NewContext(&operations, windowEvent)
			needsRefresh := state.consumeRefreshResult()
			select {
			case <-refreshTick:
				needsRefresh = true
			default:
			}
			select {
			case <-liveTick:
				state.refreshLiveSnapshot()
			default:
			}
			if state.handleInputs(gtx) {
				needsRefresh = true
			}
			if needsRefresh {
				state.requestRefresh(window)
			}
			state.layout(gtx)
			windowEvent.Frame(gtx.Ops)
		}
	}
}

func newUIState(loader func(time.Time, bool) (*dto.DashboardDTO, error)) *UIState {
	return &UIState{
		theme:    material.NewTheme(),
		load:     loader,
		results:  make(chan refreshResult, 1),
		now:      time.Now,
		location: time.Local,
		view:     viewOverview,
		use24h:   true,
		contentList: widget.List{
			List: layout.List{Axis: layout.Vertical},
		},
	}
}

func (state *UIState) handleInputs(gtx layout.Context) bool {
	refresh := false
	switch {
	case state.overviewButton.Clicked(gtx):
		state.view = viewOverview
	case state.historyButton.Clicked(gtx):
		state.view = viewHistory
	case state.healthButton.Clicked(gtx):
		state.view = viewHealth
	case state.aboutButton.Clicked(gtx):
		state.view = viewAbout
	case state.hoursButton.Clicked(gtx):
		refresh = state.setRange(true)
	case state.daysButton.Clicked(gtx):
		refresh = state.setRange(false)
	}
	if state.exportButton.Clicked(gtx) {
		if state.data == nil || len(state.data.Timeline.Points) == 0 {
			state.markExportNoData()
		} else {
			result, err := csvexport.ExportTimeline(
				state.data.Timeline.Points,
				state.use24h,
				state.now(),
			)
			state.markExportResult(result, err)
		}
	}
	for _, filter := range []key.Filter{
		{Name: key.Name("1")},
		{Name: key.Name("2")},
		{Name: key.Name("3")},
		{Name: key.Name("4")},
		{Name: key.Name("R")},
		{Name: key.NameEscape},
	} {
		for {
			raw, ok := gtx.Event(filter)
			if !ok {
				break
			}
			keyEvent, ok := raw.(key.Event)
			if !ok {
				continue
			}
			switch routeShortcut(keyEvent, false) {
			case shortcutOverview:
				state.view = viewOverview
			case shortcutHistory:
				state.view = viewHistory
			case shortcutHealth:
				state.view = viewHealth
			case shortcutAbout:
				state.view = viewAbout
			case shortcutRefresh:
				refresh = true
			case shortcutClearHover:
				state.clearHover()
			}
		}
	}
	return refresh
}

func (state *UIState) refreshLiveSnapshot() bool {
	if state.data == nil || state.liveLoad == nil {
		return false
	}
	snapshot, err := state.liveLoad("")
	if err != nil || !snapshot.Available {
		return false
	}
	state.data.LiveSnapshot = snapshot
	state.data.Live = dto.LiveBatteryDTO{
		Available:  true,
		Timestamp:  snapshot.Timestamp,
		Capacity:   snapshot.CapacityPercent,
		Status:     snapshot.Status,
		EnergyRate: snapshot.PowerRateW,
		Voltage:    snapshot.VoltageV,
	}
	if snapshot.CycleCount > 0 {
		state.data.Health.CycleCount = snapshot.CycleCount
		state.data.Health.CycleCountAvailable = true
	}
	if snapshot.ChargeLimitAvailable {
		state.data.Health.ChargeLimitPercent = snapshot.ChargeLimitPercent
		state.data.Health.ChargeLimitAvailable = true
		state.data.Health.ChargeStartThresholdPercent = snapshot.ChargeStartThresholdPercent
	}
	state.history = buildHistoryChartModel(state.data, state.use24h, state.location)
	return true
}

func (state *UIState) setRange(use24h bool) bool {
	if state.use24h == use24h {
		return false
	}
	state.use24h = use24h
	state.clearHover()
	state.clearExportFeedback()
	return true
}

func (state *UIState) requestRefresh(window *app.Window) {
	if state.loading || state.load == nil {
		return
	}
	state.loading = true
	use24h := state.use24h
	at := state.now().UTC()
	state.workers.Add(1)
	go func() {
		defer state.workers.Done()
		data, err := state.load(at, use24h)
		state.results <- refreshResult{data: data, err: err, use24h: use24h, at: at}
		window.Invalidate()
	}()
}

func (state *UIState) consumeRefreshResult() bool {
	select {
	case result := <-state.results:
		state.loading = false
		if result.use24h != state.use24h {
			return true
		}
		state.lastUpdated = result.at
		state.loadErr = result.err
		if result.err != nil {
			log.Printf("dashboard query failed: %v", result.err)
			return false
		}
		state.data = result.data
		if result.data != nil {
			state.history = buildHistoryChartModel(result.data, state.use24h, state.location)
		}
	default:
	}
	return false
}

func (state *UIState) clearHover() {
	state.batteryHovered = false
	state.hoveredPoint = nil
	state.hoveredSession = nil
	state.screenHovered = false
	state.hoveredBar = nil
	state.hoverContext = HoverContext{}
}

func (state *UIState) clearExportFeedback() {
	state.exportMessage = ""
	state.exportErr = false
}

func (state *UIState) hasData() bool {
	return state.data != nil
}

func (state *UIState) latestSampleTime() time.Time {
	if state.data == nil || !state.data.Status.Available {
		return time.Time{}
	}
	return state.data.Status.Timestamp
}

func (state *UIState) currentFreshness() freshnessView {
	now := state.now()
	if now.IsZero() {
		now = time.Now()
	}
	return sampleFreshness(state.latestSampleTime(), now)
}

func (state *UIState) markExportResult(result csvexport.ExportResult, err error) {
	if err != nil {
		state.exportMessage = "Export failed: " + err.Error()
		state.exportErr = true
		return
	}
	if result.RowCount == 0 {
		state.exportMessage = "Exported 0 rows (header only) to " + result.Path
	} else {
		state.exportMessage = fmt.Sprintf("Exported %d rows to %s", result.RowCount, result.Path)
	}
	state.exportErr = false
}

func (state *UIState) markExportNoData() {
	state.exportMessage = "No data to export"
	state.exportErr = true
}
