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

func TestBatcherDropsInvalidCapacityWithoutBuffering(t *testing.T) {
	database := openTestDB(t)
	batcher := NewBatcher(database, 10, time.Hour)
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	if err := batcher.AddTelemetry(model.Telemetry{
		Timestamp: now, Capacity: 1693139, Status: "Full", Voltage: 0,
	}); err != nil {
		t.Fatalf("add invalid telemetry should be a non-fatal drop: %v", err)
	}
	if len(batcher.buffer) != 0 {
		t.Fatalf("invalid telemetry must not stay buffered: %+v", batcher.buffer)
	}
	if err := batcher.Close(); err != nil {
		t.Fatalf("close batcher: %v", err)
	}
	points, err := database.GetTelemetryRange(now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("query telemetry: %v", err)
	}
	if len(points) != 0 {
		t.Fatalf("invalid telemetry must not be persisted: %+v", points)
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

func TestTelemetryPersistenceSkipsInvalidCapacity(t *testing.T) {
	database := openTestDB(t)
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

	if err := database.SaveTelemetryBatch([]model.Telemetry{
		{Timestamp: now, Capacity: 85, Status: "Discharging", Voltage: 8.5},
		{Timestamp: now.Add(time.Minute), Capacity: 1693139, Status: "Full", Voltage: 0},
		{Timestamp: now.Add(2 * time.Minute), Capacity: 86, Status: "Charging", Voltage: 8.6},
	}); err != nil {
		t.Fatalf("save telemetry: %v", err)
	}

	points, err := database.GetTelemetryRange(now.Add(-time.Minute), now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("query telemetry: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected invalid capacity to be skipped, got %+v", points)
	}
	for _, point := range points {
		if !point.ValidCapacity() {
			t.Fatalf("query returned invalid point: %+v", point)
		}
	}
}

func TestTelemetryQueriesIgnorePersistedInvalidCapacity(t *testing.T) {
	database := openTestDB(t)
	now := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	validBefore := model.Telemetry{Timestamp: now, Capacity: 100, Status: "Full", Voltage: 8.6}
	validAfter := model.Telemetry{Timestamp: now.Add(2 * time.Minute), Capacity: 84, Status: "Discharging", Voltage: 8.5}
	if err := database.SaveTelemetryBatch([]model.Telemetry{validBefore, validAfter}); err != nil {
		t.Fatalf("save valid telemetry: %v", err)
	}

	_, err := database.conn.Exec(
		`INSERT INTO telemetry (timestamp, capacity, status, energy_rate, voltage, screen_on)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		now.Add(time.Minute).Format(TimeFormatNano),
		1693139.0,
		"Full",
		-0.008616,
		0.0,
		1,
	)
	if err != nil {
		t.Fatalf("insert persisted invalid telemetry: %v", err)
	}

	points, err := database.GetTelemetryRange(now.Add(-time.Minute), now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("query telemetry: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected range query to hide invalid persisted telemetry, got %+v", points)
	}

	latest, err := database.GetLastTelemetryBefore(now.Add(90 * time.Second))
	if err != nil {
		t.Fatalf("get last telemetry: %v", err)
	}
	if !latest.Timestamp.Equal(validBefore.Timestamp) || latest.Capacity != validBefore.Capacity {
		t.Fatalf("expected last telemetry before invalid spike to skip it, got %+v", latest)
	}

	first, err := database.GetFirstTelemetryAfter(now.Add(30 * time.Second))
	if err != nil {
		t.Fatalf("get first telemetry: %v", err)
	}
	if !first.Timestamp.Equal(validAfter.Timestamp) || first.Capacity != validAfter.Capacity {
		t.Fatalf("expected first telemetry after invalid spike to skip it, got %+v", first)
	}

	full, err := database.GetLastFullChargeBefore(now.Add(90 * time.Second))
	if err != nil {
		t.Fatalf("get last full charge: %v", err)
	}
	if !full.Timestamp.Equal(validBefore.Timestamp) || full.Capacity != validBefore.Capacity {
		t.Fatalf("expected last full charge to skip invalid spike, got %+v", full)
	}
}
