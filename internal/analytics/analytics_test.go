package analytics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"bati/internal/db"
	"bati/internal/model"

	_ "modernc.org/sqlite"
)

func TestOvernightDurationRequiresFourHours(t *testing.T) {
	if isSufficientOvernightDuration(4*time.Hour - time.Second) {
		t.Fatal("duration below four hours must not produce an overnight report")
	}
	if !isSufficientOvernightDuration(4 * time.Hour) {
		t.Fatal("four hours must be sufficient for an overnight report")
	}
}

func TestCalculateOvernightDrain_ExplicitSleepResume(t *testing.T) {
	// Create a temp database for testing
	tempDir, err := os.MkdirTemp("", "bati-test-*")
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

	now := time.Now().UTC()
	sleepTime := now.Add(-10 * time.Hour)
	resumeTime := now.Add(-2 * time.Hour)

	// Seed telemetry points around sleep and resume times
	telemetryData := []model.Telemetry{
		{
			Timestamp:  sleepTime.Add(-1 * time.Minute), // 60s away
			Capacity:   80.0,
			Status:     "Discharging",
			EnergyRate: 5.2,
			Voltage:    15.4,
			ScreenOn:   true,
		},
		{
			Timestamp:  sleepTime.Add(10 * time.Second), // 10s away (closest)
			Capacity:   79.0,
			Status:     "Discharging",
			EnergyRate: 0.1,
			Voltage:    15.3,
			ScreenOn:   false,
		},
		{
			Timestamp:  resumeTime.Add(-10 * time.Second), // 10s away (closest)
			Capacity:   74.0,
			Status:     "Discharging",
			EnergyRate: 0.1,
			Voltage:    15.2,
			ScreenOn:   false,
		},
		{
			Timestamp:  resumeTime.Add(1 * time.Minute), // 60s away
			Capacity:   73.0,
			Status:     "Discharging",
			EnergyRate: 6.5,
			Voltage:    15.1,
			ScreenOn:   true,
		},
	}

	if err := database.SaveTelemetryBatch(telemetryData); err != nil {
		t.Fatalf("failed to save telemetry data: %v", err)
	}

	// Seed sleep and resume events
	sleepEvent := model.Event{
		Timestamp: sleepTime,
		Type:      "sleep",
	}
	resumeEvent := model.Event{
		Timestamp: resumeTime,
		Type:      "resume",
	}

	if err := database.SaveEvent(sleepEvent); err != nil {
		t.Fatalf("failed to save sleep event: %v", err)
	}
	if err := database.SaveEvent(resumeEvent); err != nil {
		t.Fatalf("failed to save resume event: %v", err)
	}

	// Run overnight drain calculation
	report, err := CalculateOvernightDrain(database)
	if err != nil {
		t.Fatalf("failed to calculate overnight drain: %v", err)
	}

	if report.Type != "Overnight Drain (Sleep Event Observed)" {
		t.Errorf("expected report type to be 'Overnight Drain (Sleep Event Observed)', got %s", report.Type)
	}

	if report.Provenance != "observed" {
		t.Errorf("expected provenance 'observed', got %s", report.Provenance)
	}

	if report.StartPct != 79.0 {
		t.Errorf("expected StartPct to be 79.0, got %.1f", report.StartPct)
	}

	if report.EndPct != 74.0 {
		t.Errorf("expected EndPct to be 74.0, got %.1f", report.EndPct)
	}

	expectedDrain := -5.0
	if report.Drain != expectedDrain {
		t.Errorf("expected Drain to be %.1f, got %.1f", expectedDrain, report.Drain)
	}

	expectedDuration := 8 * time.Hour
	if report.Duration != expectedDuration {
		t.Errorf("expected Duration to be %v, got %v", expectedDuration, report.Duration)
	}
}

func TestCalculateOvernightDrain_IdleFallback(t *testing.T) {
	// Create a temp database for testing
	tempDir, err := os.MkdirTemp("", "bati-test-*")
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

	now := time.Now().UTC()
	idleStart := now.Add(-8 * time.Hour)
	idleEnd := now.Add(-2 * time.Hour)

	// Seed telemetry points with screen_on = false for a duration > 4h
	telemetryData := []model.Telemetry{
		{
			Timestamp:  idleStart.Add(-10 * time.Minute),
			Capacity:   90.0,
			Status:     "Discharging",
			EnergyRate: 5.0,
			Voltage:    15.4,
			ScreenOn:   true,
		},
		{
			Timestamp:  idleStart,
			Capacity:   89.0,
			Status:     "Discharging",
			EnergyRate: 1.2,
			Voltage:    15.3,
			ScreenOn:   false,
		},
		{
			Timestamp:  idleStart.Add(2 * time.Hour),
			Capacity:   87.0,
			Status:     "Discharging",
			EnergyRate: 1.1,
			Voltage:    15.2,
			ScreenOn:   false,
		},
		{
			Timestamp:  idleEnd,
			Capacity:   85.0,
			Status:     "Discharging",
			EnergyRate: 1.1,
			Voltage:    15.1,
			ScreenOn:   false,
		},
		{
			Timestamp:  idleEnd.Add(10 * time.Minute), // Gap extends until active screen reading
			Capacity:   84.0,
			Status:     "Discharging",
			EnergyRate: 5.5,
			Voltage:    15.0,
			ScreenOn:   true,
		},
	}

	if err := database.SaveTelemetryBatch(telemetryData); err != nil {
		t.Fatalf("failed to save telemetry data: %v", err)
	}

	// Run overnight drain calculation (expecting fallback to screen_on = false gap)
	report, err := CalculateOvernightDrain(database)
	if err != nil {
		t.Fatalf("failed to calculate overnight drain: %v", err)
	}

	if report.Type != "Estimated Overnight Drain (Screen-off Fallback)" {
		t.Errorf("expected report type to be 'Estimated Overnight Drain (Screen-off Fallback)', got %s", report.Type)
	}

	if report.Provenance != "estimated" {
		t.Errorf("expected provenance 'estimated', got %s", report.Provenance)
	}

	if report.StartPct != 89.0 {
		t.Errorf("expected StartPct to be 89.0, got %.1f", report.StartPct)
	}

	// The idle gap ends when screen_on becomes true (at idleEnd + 10m, capacity 84.0)
	if report.EndPct != 84.0 {
		t.Errorf("expected EndPct to be 84.0, got %.1f", report.EndPct)
	}

	expectedDrain := -5.0
	if report.Drain != expectedDrain {
		t.Errorf("expected Drain to be %.1f, got %.1f", expectedDrain, report.Drain)
	}

	expectedDuration := 6*time.Hour + 10*time.Minute
	if report.Duration != expectedDuration {
		t.Errorf("expected Duration to be %v, got %v", expectedDuration, report.Duration)
	}
}
