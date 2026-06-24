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

func TestGenerateSessions_And_Summary(t *testing.T) {
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

	// 12 hours timeline seed (running on UTC for database matching)
	now := time.Now().UTC()
	baseTime := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, time.UTC)

	// Seed telemetry:
	// 08:00 - 10:00 (Discharging, screen on)
	// 10:00 - 12:00 (Discharging, screen off)
	// 12:00 - 13:00 (Charging, screen off)
	telemetryData := []model.Telemetry{
		// De-charging screen active
		{Timestamp: baseTime, Capacity: 90.0, Status: "Discharging", EnergyRate: 5.0, Voltage: 15.4, ScreenOn: true},
		{Timestamp: baseTime.Add(1 * time.Hour), Capacity: 85.0, Status: "Discharging", EnergyRate: 5.0, Voltage: 15.3, ScreenOn: true},
		// De-charging screen inactive
		{Timestamp: baseTime.Add(2 * time.Hour), Capacity: 80.0, Status: "Discharging", EnergyRate: 1.5, Voltage: 15.2, ScreenOn: false},
		{Timestamp: baseTime.Add(3 * time.Hour), Capacity: 78.0, Status: "Discharging", EnergyRate: 1.5, Voltage: 15.1, ScreenOn: false},
		// Charging screen inactive
		{Timestamp: baseTime.Add(4 * time.Hour), Capacity: 78.0, Status: "Charging", EnergyRate: -15.0, Voltage: 15.5, ScreenOn: false},
		{Timestamp: baseTime.Add(5 * time.Hour), Capacity: 95.0, Status: "Charging", EnergyRate: -15.0, Voltage: 15.6, ScreenOn: false},
	}

	if err := database.SaveTelemetryBatch(telemetryData); err != nil {
		t.Fatalf("failed to save telemetry: %v", err)
	}

	// Sleep interval event: 11:30 - 11:45 (inside the discharging period)
	sleepTime := baseTime.Add(3*time.Hour + 30*time.Minute)  // 11:30
	resumeTime := baseTime.Add(3*time.Hour + 45*time.Minute) // 11:45

	// Seed close telemetry points for sleep capacity calculations
	sleepTelemetry := []model.Telemetry{
		{Timestamp: sleepTime, Capacity: 78.0, Status: "Discharging", EnergyRate: 0.1, Voltage: 15.1, ScreenOn: false},
		{Timestamp: resumeTime, Capacity: 77.0, Status: "Discharging", EnergyRate: 0.1, Voltage: 15.1, ScreenOn: false},
	}
	if err := database.SaveTelemetryBatch(sleepTelemetry); err != nil {
		t.Fatalf("failed to save sleep telemetry: %v", err)
	}

	if err := database.SaveEvent(model.Event{Timestamp: sleepTime, Type: "sleep"}); err != nil {
		t.Fatalf("failed to save sleep event: %v", err)
	}
	if err := database.SaveEvent(model.Event{Timestamp: resumeTime, Type: "resume"}); err != nil {
		t.Fatalf("failed to save resume event: %v", err)
	}

	// Calculate daily summary for baseTime
	summary, err := GenerateDailySummary(database, baseTime)
	if err != nil {
		t.Fatalf("failed to calculate daily summary: %v", err)
	}
	// Verify session splits
	var sleepSessionCount, chargingSessionCount, dischargingSessionCount int
	for _, s := range summary.Sessions {
		if s.Provenance != "observed" && s.Provenance != "inferred" {
			t.Errorf("expected provenance 'observed' or 'inferred', got %s", s.Provenance)
		}

		switch s.Type {
		case "sleeping":
			sleepSessionCount++
			if s.Duration != 15*time.Minute {
				t.Errorf("expected sleep duration 15m, got %v", s.Duration)
			}
			if s.DeltaPct != -1.0 {
				t.Errorf("expected sleep delta -1.0%%, got %.1f%%", s.DeltaPct)
			}
		case "charging":
			chargingSessionCount++
		case "discharging":
			dischargingSessionCount++
		}
	}

	if sleepSessionCount != 1 {
		t.Errorf("expected 1 sleeping session, got %d", sleepSessionCount)
	}
	if chargingSessionCount != 1 {
		t.Errorf("expected 1 charging session, got %d", chargingSessionCount)
	}
	if dischargingSessionCount != 2 {
		t.Errorf("expected 2 discharging sessions (split by sleep), got %d", dischargingSessionCount)
	}

	if summary.ActiveDuration == 0 {
		t.Errorf("expected non-zero active duration")
	}
}

func TestFullStateDoesNotCreateDischargingSession(t *testing.T) {
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
	baseTime := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)

	// Seed telemetry representing battery fully charged (Full state)
	telemetryData := []model.Telemetry{
		{Timestamp: baseTime, Capacity: 100.0, Status: "Full", EnergyRate: 0.0, Voltage: 16.8, ScreenOn: true},
		{Timestamp: baseTime.Add(30 * time.Minute), Capacity: 100.0, Status: "Full", EnergyRate: 0.0, Voltage: 16.8, ScreenOn: true},
	}

	if err := database.SaveTelemetryBatch(telemetryData); err != nil {
		t.Fatalf("failed to save telemetry: %v", err)
	}

	summary, err := GenerateDailySummary(database, baseTime)
	if err != nil {
		t.Fatalf("failed to calculate daily summary: %v", err)
	}

	if len(summary.Sessions) != 1 {
		t.Fatalf("expected exactly 1 session, got %d", len(summary.Sessions))
	}

	s := summary.Sessions[0]
	if s.Type != "full" {
		t.Errorf("expected session type to be 'full', got %s", s.Type)
	}

	if s.DeltaPct != 0.0 {
		t.Errorf("expected session DeltaPct to be 0.0, got %.1f", s.DeltaPct)
	}

	if summary.TotalDischarge != 0.0 {
		t.Errorf("expected total discharge to be 0.0, got %.1f", summary.TotalDischarge)
	}
}

func TestNotChargingStateCreatesNotChargingSession(t *testing.T) {
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
	baseTime := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)

	// Seed telemetry representing battery held at threshold (Not charging state)
	telemetryData := []model.Telemetry{
		{Timestamp: baseTime, Capacity: 80.0, Status: "Not charging", EnergyRate: 0.0, Voltage: 15.8, ScreenOn: true},
		{Timestamp: baseTime.Add(30 * time.Minute), Capacity: 80.0, Status: "Not charging", EnergyRate: 0.0, Voltage: 15.8, ScreenOn: true},
	}

	if err := database.SaveTelemetryBatch(telemetryData); err != nil {
		t.Fatalf("failed to save telemetry: %v", err)
	}

	summary, err := GenerateDailySummary(database, baseTime)
	if err != nil {
		t.Fatalf("failed to calculate daily summary: %v", err)
	}

	if len(summary.Sessions) != 1 {
		t.Fatalf("expected exactly 1 session, got %d", len(summary.Sessions))
	}

	s := summary.Sessions[0]
	if s.Type != "not_charging" {
		t.Errorf("expected session type to be 'not_charging', got %s", s.Type)
	}

	if s.DeltaPct != 0.0 {
		t.Errorf("expected session DeltaPct to be 0.0, got %.1f", s.DeltaPct)
	}

	if summary.TotalDischarge != 0.0 {
		t.Errorf("expected total discharge to be 0.0, got %.1f", summary.TotalDischarge)
	}
}

func TestShutdownBootGapCutsKnownSessions(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.Open(filepath.Join(tempDir, "shutdown-gap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitSchema(); err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	shutdown := start.Add(20 * time.Minute)
	boot := start.Add(90 * time.Minute)
	beforeShutdown := shutdown.Add(-time.Minute)
	afterBoot := boot.Add(time.Minute)
	rangeEnd := boot.Add(10 * time.Minute)

	if err := database.SaveTelemetryBatch([]model.Telemetry{
		{Timestamp: start, Capacity: 85, Status: "Not charging", ScreenOn: true},
		{Timestamp: beforeShutdown, Capacity: 85, Status: "Not charging", ScreenOn: true},
		{Timestamp: afterBoot, Capacity: 84, Status: "Not charging", ScreenOn: true},
		{Timestamp: rangeEnd, Capacity: 84, Status: "Not charging", ScreenOn: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveEvent(model.Event{Timestamp: shutdown, Type: "shutdown"}); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveEvent(model.Event{Timestamp: boot, Type: "boot"}); err != nil {
		t.Fatal(err)
	}

	sessions, err := GenerateSessions(database, start, rangeEnd)
	if err != nil {
		t.Fatal(err)
	}
	var notCharging []Session
	for _, session := range sessions {
		if session.StartTime.Before(shutdown) && session.EndTime.After(boot) {
			t.Fatalf("session must not span shutdown/boot gap: %+v", session)
		}
		if session.Type == "not_charging" {
			notCharging = append(notCharging, session)
		}
	}
	if len(notCharging) != 2 {
		t.Fatalf("expected sessions on both sides of the offline gap, got %+v", sessions)
	}
	if !notCharging[0].EndTime.Equal(beforeShutdown) {
		t.Fatalf("first session should stop at last telemetry before shutdown, got %+v", notCharging[0])
	}
	if !notCharging[1].StartTime.Equal(afterBoot) {
		t.Fatalf("second session should start at first telemetry after boot, got %+v", notCharging[1])
	}

	summary, err := GenerateRangeSummary(database, start, rangeEnd)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalDischarge != 0 || summary.TotalCharge != 0 {
		t.Fatalf("offline capacity delta must not be counted as measured activity, got discharge=%.1f charge=%.1f", summary.TotalDischarge, summary.TotalCharge)
	}
}

func TestSleepSessionExistsWithoutTelemetryDuringSuspend(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.Open(filepath.Join(tempDir, "sleep-gap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitSchema(); err != nil {
		t.Fatal(err)
	}

	start := time.Now().UTC().Add(-12 * time.Hour)
	sleepStart := start.Add(2 * time.Hour)
	resume := sleepStart.Add(7 * time.Hour)
	if err := database.SaveTelemetryBatch([]model.Telemetry{
		{Timestamp: start, Capacity: 82, Status: "Discharging", ScreenOn: true},
		{Timestamp: sleepStart.Add(-5 * time.Minute), Capacity: 78, Status: "Discharging", ScreenOn: false},
		{Timestamp: resume.Add(5 * time.Minute), Capacity: 74, Status: "Discharging", ScreenOn: true},
		{Timestamp: resume.Add(time.Hour), Capacity: 70, Status: "Discharging", ScreenOn: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveEvent(model.Event{Timestamp: sleepStart, Type: "sleep"}); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveEvent(model.Event{Timestamp: resume, Type: "resume"}); err != nil {
		t.Fatal(err)
	}

	sessions, err := GenerateSessions(database, start, resume.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var sleep *Session
	for index := range sessions {
		if sessions[index].Type == "sleeping" {
			sleep = &sessions[index]
			break
		}
	}
	if sleep == nil {
		t.Fatalf("explicit sleep/resume events must produce a sleep session: %+v", sessions)
	}
	if sleep.Duration != 7*time.Hour || sleep.StartPct != 78 || sleep.EndPct != 74 || sleep.DeltaPct != -4 {
		t.Fatalf("unexpected sleep session: %+v", sleep)
	}
	if sleep.Provenance != "observed" {
		t.Fatalf("explicit sleep event must remain observed, got %q", sleep.Provenance)
	}
}
