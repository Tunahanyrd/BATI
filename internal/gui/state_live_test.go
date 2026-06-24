package gui

import (
	"testing"
	"time"

	"bati/internal/dto"
)

func TestRefreshLiveSnapshotUpdatesOverviewAndHistoryCurrent(t *testing.T) {
	now := time.Date(2026, 6, 24, 10, 45, 0, 0, time.UTC)
	state := newUIState(nil)
	state.data = &dto.DashboardDTO{
		LiveSnapshot: dto.LiveSnapshotDTO{
			Available:       true,
			CapacityPercent: 93,
			Status:          "Not charging",
		},
		HistoricalSnapshot: dto.HistoricalSnapshotDTO{
			Available:       true,
			Stale:           true,
			CapacityPercent: 100,
			Status:          "Full",
			Age:             24 * time.Hour,
		},
		Timeline: dto.TimelineDTO{
			Start: now.Add(-time.Hour),
			End:   now,
			Points: []dto.TimelinePointDTO{
				{Timestamp: now.Add(-30 * time.Minute), Capacity: 93, Status: "Not charging"},
				{Timestamp: now.Add(-10 * time.Minute), Capacity: 92, Status: "Discharging"},
			},
		},
	}
	state.history = buildHistoryChartModel(state.data, true, time.UTC)
	state.liveLoad = func(string) (dto.LiveSnapshotDTO, error) {
		return dto.LiveSnapshotDTO{
			Available:            true,
			Timestamp:            now,
			CapacityPercent:      92,
			Status:               "Discharging",
			PowerRateW:           6.25,
			PowerRateAvailable:   true,
			VoltageV:             8.55,
			VoltageAvailable:     true,
			CycleCount:           5,
			ChargeLimitAvailable: true,
			ChargeLimitPercent:   80,
		}, nil
	}

	if !state.refreshLiveSnapshot() {
		t.Fatal("expected live refresh to update state")
	}
	if state.data.LiveSnapshot.CapacityPercent != 92 || state.data.LiveSnapshot.PowerRateW != 6.25 {
		t.Fatalf("live snapshot was not updated: %+v", state.data.LiveSnapshot)
	}
	if state.data.Live.Capacity != 92 || state.data.Live.EnergyRate != 6.25 {
		t.Fatalf("compat live DTO was not updated: %+v", state.data.Live)
	}
	if !state.data.Health.CycleCountAvailable || state.data.Health.CycleCount != 5 {
		t.Fatalf("health cycle count was not refreshed: %+v", state.data.Health)
	}
	if !state.history.Current.Available || state.history.Current.CapacityPercent != 92 {
		t.Fatalf("history current marker was not rebuilt: %+v", state.history.Current)
	}
}

func TestRefreshLiveSnapshotIgnoresUnavailableLiveState(t *testing.T) {
	state := newUIState(nil)
	state.data = &dto.DashboardDTO{}
	state.liveLoad = func(string) (dto.LiveSnapshotDTO, error) {
		return dto.LiveSnapshotDTO{}, nil
	}
	if state.refreshLiveSnapshot() {
		t.Fatal("expected unavailable live state to be ignored")
	}
}
