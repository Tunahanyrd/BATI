package diagnostics

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bati/internal/db"
	"bati/internal/model"

	_ "modernc.org/sqlite"
)

type fakeServiceChecker struct {
	state ServiceState
}

func (checker fakeServiceChecker) Check(context.Context) ServiceState {
	return checker.state
}

func openDiagnosticsTestDB(t *testing.T) (*db.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "bati-test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	if err := database.InitSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return database, dbPath
}

func TestSnapshotReportsMissingServiceAndDatabase(t *testing.T) {
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	status := Snapshot(nil, filepath.Join(t.TempDir(), "missing.db"), now, fakeServiceChecker{
		state: ServiceState{LoadState: "not-found", ActiveState: "inactive", SubState: "dead"},
	})

	if status.DBExists {
		t.Fatal("expected missing database")
	}
	if status.Service.LoadState != "not-found" {
		t.Fatalf("expected service not-found, got %+v", status.Service)
	}
	if !strings.Contains(status.Recommendation, "install-user-service.sh") {
		t.Fatalf("expected install recommendation, got %q", status.Recommendation)
	}
	if status.Live.Available || status.Live.Err == "" {
		t.Fatalf("expected live sysfs to be unavailable in tests, got %+v", status.Live)
	}
}

func TestSnapshotReportsInactiveServiceBeforeDatabaseAdvice(t *testing.T) {
	database, dbPath := openDiagnosticsTestDB(t)
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	status := Snapshot(database, dbPath, now, fakeServiceChecker{
		state: ServiceState{LoadState: "loaded", ActiveState: "failed", SubState: "failed"},
	})

	if !strings.Contains(status.Recommendation, "systemctl --user enable --now batid.service") {
		t.Fatalf("expected start recommendation, got %q", status.Recommendation)
	}
}

func TestSnapshotReportsEmptyStaleAndFreshDatabaseStates(t *testing.T) {
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	activeService := fakeServiceChecker{
		state: ServiceState{LoadState: "loaded", ActiveState: "active", SubState: "running"},
	}

	emptyDB, emptyPath := openDiagnosticsTestDB(t)
	empty := Snapshot(emptyDB, emptyPath, now, activeService)
	if !empty.DBExists || empty.LatestSampleAvailable {
		t.Fatalf("expected existing empty db with no latest sample, got %+v", empty)
	}
	if !strings.Contains(empty.Recommendation, "first sample") {
		t.Fatalf("expected first sample recommendation, got %q", empty.Recommendation)
	}

	staleDB, stalePath := openDiagnosticsTestDB(t)
	if err := staleDB.SaveTelemetryBatch([]model.Telemetry{
		{Timestamp: now.Add(-20 * time.Minute), Capacity: 93, Status: "Not charging", Voltage: 8.6},
	}); err != nil {
		t.Fatalf("save stale telemetry: %v", err)
	}
	stale := Snapshot(staleDB, stalePath, now, activeService)
	if !stale.LatestSampleAvailable || !stale.LatestSampleStale {
		t.Fatalf("expected stale latest sample, got %+v", stale)
	}
	if !strings.Contains(stale.Recommendation, "not recording fresh samples") {
		t.Fatalf("expected stale recommendation, got %q", stale.Recommendation)
	}

	freshDB, freshPath := openDiagnosticsTestDB(t)
	if err := freshDB.SaveTelemetryBatch([]model.Telemetry{
		{Timestamp: now.Add(-2 * time.Minute), Capacity: 93, Status: "Not charging", Voltage: 8.6},
	}); err != nil {
		t.Fatalf("save fresh telemetry: %v", err)
	}
	fresh := Snapshot(freshDB, freshPath, now, activeService)
	if !fresh.LatestSampleAvailable || fresh.LatestSampleStale {
		t.Fatalf("expected fresh latest sample, got %+v", fresh)
	}
	if fresh.TodaySampleCount != 1 {
		t.Fatalf("expected one sample today, got %d", fresh.TodaySampleCount)
	}
	if fresh.Recommendation != "Recording looks healthy." {
		t.Fatalf("expected healthy recommendation, got %q", fresh.Recommendation)
	}
}

func TestServiceStateSummary(t *testing.T) {
	state := ServiceState{LoadState: "loaded", ActiveState: "active", SubState: "running"}
	if got := state.Summary(); got != "load=loaded active=active sub=running" {
		t.Fatalf("unexpected summary: %q", got)
	}
}

func TestLiveBatterySummaryUsesDisplayStatus(t *testing.T) {
	live := LiveBattery{
		Available:            true,
		DeviceID:             "BAT0",
		CapacityPercent:      71,
		Status:               "Discharging",
		ChargeLimitAvailable: true,
		ChargeLimitPercent:   80,
	}
	if got := live.Summary(); got != "BAT0 71% · Discharging... · charge limit 80%" {
		t.Fatalf("unexpected live summary: %q", got)
	}
}
