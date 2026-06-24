package analytics

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"bati/internal/db"
	"bati/internal/model"

	_ "modernc.org/sqlite"
)

func TestHeavyVolume_And_Concurrency(t *testing.T) {
	// Create a temp database for stress testing
	tempDir, err := os.MkdirTemp("", "bati-heavy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "heavy_test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	if err := database.InitSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	// 1. Stress Test: Concurrency and Thread Safety
	// Spin up multiple goroutines writing to the batcher simultaneously
	batcher := db.NewBatcher(database, 50, 100*time.Millisecond)

	numGoroutines := 15
	pointsPerRoutine := 500
	var wgConcurrent sync.WaitGroup

	errs := make(chan error, numGoroutines*2)

	// Base time for concurrent writes (spread over time to avoid duplicate key issues if not nanosecond)
	baseTime := time.Now().UTC().Add(-48 * time.Hour)

	for g := 0; g < numGoroutines; g++ {
		wgConcurrent.Add(2)

		// Telemetry writer
		go func(id int) {
			defer wgConcurrent.Done()
			for i := 0; i < pointsPerRoutine; i++ {
				// Each telemetry point gets a unique nanosecond offset to simulate real-time separation
				ts := baseTime.Add(time.Duration(id)*time.Hour + time.Duration(i)*time.Second + time.Duration(rand.Intn(1000))*time.Nanosecond)
				pt := model.Telemetry{
					Timestamp:  ts,
					Capacity:   100.0 - float64(i%100),
					Status:     "Discharging",
					EnergyRate: 5.5,
					Voltage:    15.4,
					ScreenOn:   true,
				}
				if err := batcher.AddTelemetry(pt); err != nil {
					errs <- fmt.Errorf("routine %d failed to add telemetry: %w", id, err)
					return
				}
			}
		}(g)

		// Event writer
		go func(id int) {
			defer wgConcurrent.Done()
			for i := 0; i < 20; i++ {
				ts := baseTime.Add(time.Duration(id)*time.Hour + time.Duration(i*30)*time.Second + time.Duration(rand.Intn(1000))*time.Nanosecond)
				evt := model.Event{
					Timestamp: ts,
					Type:      "screen_off",
					Payload:   fmt.Sprintf("routine_%d_event_%d", id, i),
				}
				if err := batcher.SaveEvent(evt); err != nil {
					errs <- fmt.Errorf("routine %d failed to save event: %w", id, err)
					return
				}
			}
		}(g)
	}

	wgConcurrent.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrency error: %v", err)
	}

	// Close batcher to flush remaining points
	if err := batcher.Close(); err != nil {
		t.Fatalf("failed to close batcher: %v", err)
	}

	// Verify count of records
	points, err := database.GetTelemetryRange(baseTime.Add(-1*time.Hour), time.Now().UTC().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("get telemetry range: %v", err)
	}
	expectedCount := numGoroutines * pointsPerRoutine
	if len(points) != expectedCount {
		t.Errorf("expected %d telemetry points, got %d", expectedCount, len(points))
	}

	// 2. Performance Stress Test: High-volume dataset parsing
	// Seed a very large continuous dataset (15,000+ points)
	// We want to simulate 14 days of telemetry data at 1-minute intervals
	t.Log("Seeding high-volume data...")
	performanceBase := time.Now().UTC().Add(-14 * 24 * time.Hour)
	pointsCount := 15000
	largeTelemetry := make([]model.Telemetry, pointsCount)
	largeEvents := make([]model.Event, 0)

	capacity := 100.0
	status := "Discharging"
	screenOn := true

	for i := 0; i < pointsCount; i++ {
		ts := performanceBase.Add(time.Duration(i) * time.Minute)

		// Simulate battery drainage and charging cycles
		if status == "Discharging" {
			capacity -= 0.1
			if capacity <= 20.0 {
				status = "Charging"
				// Add an event
				largeEvents = append(largeEvents, model.Event{Timestamp: ts, Type: "ac_connected"})
			}
		} else {
			capacity += 0.3
			if capacity >= 80.0 { // threshold charging limit
				status = "Not charging"
			}
		}

		if status == "Not charging" {
			// Hold at 80% for some time
			if i%200 == 0 {
				status = "Discharging"
				largeEvents = append(largeEvents, model.Event{Timestamp: ts, Type: "ac_disconnected"})
			}
		}

		// Insert periodic sleep intervals (e.g. overnight 8 hours sleep every 1440 minutes)
		if i > 0 && i%1440 == 0 {
			sleepStart := ts
			resumeTime := ts.Add(8 * time.Hour)
			largeEvents = append(largeEvents, model.Event{Timestamp: sleepStart, Type: "sleep"})
			largeEvents = append(largeEvents, model.Event{Timestamp: resumeTime, Type: "resume"})
			// fast forward i by 480 minutes (8 hours)
			for step := 0; step < 480 && i < pointsCount-1; step++ {
				i++
				largeTelemetry[i] = model.Telemetry{
					Timestamp:  performanceBase.Add(time.Duration(i) * time.Minute),
					Capacity:   capacity,
					Status:     status,
					EnergyRate: 0.0,
					Voltage:    15.0,
					ScreenOn:   false,
				}
			}
			ts = resumeTime
		}

		if i < pointsCount {
			largeTelemetry[i] = model.Telemetry{
				Timestamp:  ts,
				Capacity:   capacity,
				Status:     status,
				EnergyRate: 1.5,
				Voltage:    15.1,
				ScreenOn:   screenOn,
			}
		}
	}

	// Save the batch to database
	err = database.SaveTelemetryBatch(largeTelemetry)
	if err != nil {
		t.Fatalf("failed to save large telemetry batch: %v", err)
	}

	for _, ev := range largeEvents {
		if err := database.SaveEvent(ev); err != nil {
			t.Fatalf("failed to save large event: %v", err)
		}
	}

	t.Log("High-volume data seeded. Running segmentation performance benchmarks...")

	// Benchmark: GenerateSessions over 14 days of data (15,000 telemetry points)
	startTime := time.Now()
	sessions, err := GenerateSessions(database, performanceBase, time.Now().UTC())
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("GenerateSessions failed on large dataset: %v", err)
	}

	t.Logf("Generated %d sessions over 14 days in %v", len(sessions), elapsed)

	// Invariant: Session processing for 14 days of highly dense data must take < 200ms under normal conditions
	limit := 3 * time.Second
	if elapsed > limit {
		t.Errorf("performance check failed: GenerateSessions took %v (threshold: %v)", elapsed, limit)
	} else if elapsed > 200*time.Millisecond {
		t.Logf("Warning: GenerateSessions took %v (normal run threshold is 200ms, but allowed up to 3s for race detector/slow environment)", elapsed)
	}

	// Benchmark: Daily Summary Generation
	startTime = time.Now()
	summary, err := GenerateRangeSummary(database, performanceBase, time.Now().UTC())
	elapsedSummary := time.Since(startTime)

	if err != nil {
		t.Fatalf("GenerateRangeSummary failed on large dataset: %v", err)
	}

	t.Logf("Generated range summary in %v. Active Screen Time: %s", elapsedSummary, summary.ActiveDuration)
	if elapsedSummary > limit {
		t.Errorf("performance check failed: GenerateRangeSummary took %v (threshold: %v)", elapsedSummary, limit)
	} else if elapsedSummary > 200*time.Millisecond {
		t.Logf("Warning: GenerateRangeSummary took %v (normal run threshold is 200ms, but allowed up to 3s for race detector/slow environment)", elapsedSummary)
	}
}
