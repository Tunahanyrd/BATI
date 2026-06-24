package db

import (
	"path/filepath"
	"testing"
	"time"

	"bati/internal/model"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "bati-test.db"))
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
	return database
}

func TestBatcherCloseFlushesBufferedTelemetry(t *testing.T) {
	database := openTestDB(t)
	batcher := NewBatcher(database, 10, time.Hour)
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	if err := batcher.AddTelemetry(model.Telemetry{
		Timestamp: now, Capacity: 93, Status: "Not charging", Voltage: 8.6,
	}); err != nil {
		t.Fatalf("add telemetry: %v", err)
	}
	beforeClose, err := database.GetTelemetryRange(now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("query before close: %v", err)
	}
	if len(beforeClose) != 0 {
		t.Fatalf("expected telemetry to remain buffered before close, got %d rows", len(beforeClose))
	}

	if err := batcher.Close(); err != nil {
		t.Fatalf("close batcher: %v", err)
	}
	afterClose, err := database.GetTelemetryRange(now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("query after close: %v", err)
	}
	if len(afterClose) != 1 {
		t.Fatalf("expected close to flush one row, got %d", len(afterClose))
	}
}

func TestBatcherSaveEventFlushesBufferedTelemetryTransactionally(t *testing.T) {
	database := openTestDB(t)
	batcher := NewBatcher(database, 10, time.Hour)
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	if err := batcher.AddTelemetry(model.Telemetry{
		Timestamp: now, Capacity: 93, Status: "Not charging", Voltage: 8.6,
	}); err != nil {
		t.Fatalf("add telemetry: %v", err)
	}
	if err := batcher.SaveEvent(model.Event{Timestamp: now.Add(time.Second), Type: "screen_on"}); err != nil {
		t.Fatalf("save event: %v", err)
	}

	points, err := database.GetTelemetryRange(now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("query telemetry: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected event save to flush buffered telemetry, got %d rows", len(points))
	}
	events, err := database.GetEventsRange(now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	if len(events) != 1 || events[0].Type != "screen_on" {
		t.Fatalf("expected saved screen event, got %+v", events)
	}
}

func TestSaveTelemetryBatchReplacesDuplicateTimestamp(t *testing.T) {
	database := openTestDB(t)
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	if err := database.SaveTelemetryBatch([]model.Telemetry{
		{Timestamp: now, Capacity: 90, Status: "Discharging", Voltage: 8.5},
		{Timestamp: now, Capacity: 91, Status: "Charging", Voltage: 8.6},
	}); err != nil {
		t.Fatalf("save telemetry: %v", err)
	}

	points, err := database.GetTelemetryRange(now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("query telemetry: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("expected duplicate timestamp to replace to one row, got %d", len(points))
	}
	if points[0].Capacity != 91 || points[0].Status != "Charging" {
		t.Fatalf("expected latest duplicate values to win, got %+v", points[0])
	}
}
