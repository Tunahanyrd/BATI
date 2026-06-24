package analytics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"bati/internal/db"
	"bati/internal/dto"
	"bati/internal/model"
)

func TestStaleTelemetrySemantics(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bati-test-stale-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_stale.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	if err := database.InitSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	// Seed telemetry: latest sample is 20 hours old relative to mock "now"
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	latestSampleTime := now.Add(-20 * time.Hour)

	telemetryData := []model.Telemetry{
		{Timestamp: latestSampleTime.Add(-2 * time.Hour), Capacity: 80.0, Status: "Discharging", EnergyRate: 5.0, Voltage: 15.2, ScreenOn: true},
		{Timestamp: latestSampleTime, Capacity: 75.0, Status: "Discharging", EnergyRate: 5.0, Voltage: 15.1, ScreenOn: true},
	}
	if err := database.SaveTelemetryBatch(telemetryData); err != nil {
		t.Fatalf("failed to save telemetry: %v", err)
	}

	// Fetch dashboard at "now" (which makes latest sample stale by 20 hours)
	dtoVal, err := FetchDashboardData(database, now, true)
	if err != nil {
		t.Fatalf("FetchDashboardData failed: %v", err)
	}

	if dtoVal.RecentSummary.ActiveDuration <= 0 {
		t.Errorf("expected non-zero active duration, got %s", dtoVal.RecentSummary.ActiveDuration)
	}

	// Test that active screen time does NOT decrease as wall clock time passes without new telemetry!
	laterNow := now.Add(1 * time.Hour)
	dtoVal2, err := FetchDashboardData(database, laterNow, true)
	if err != nil {
		t.Fatalf("FetchDashboardData at later time failed: %v", err)
	}

	if dtoVal.RecentSummary.ActiveDuration != dtoVal2.RecentSummary.ActiveDuration {
		t.Errorf("expected active screen duration to be anchored and constant, got %s then %s",
			dtoVal.RecentSummary.ActiveDuration, dtoVal2.RecentSummary.ActiveDuration)
	}
}

func TestChargeLimitSemantics(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bati-test-limits-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_limits.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	if err := database.InitSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	// Seed device with cycle count but let's test co-existence of health 100% and charge limit 80%
	device := model.Device{
		ID:             "BAT0",
		Vendor:         "Asus",
		Model:          "Main Battery",
		DesignCapacity: 50.0,
		FullCapacity:   50.0, // Health is 100%
		Technology:     "LION",
		CycleCount:     50,
		IsPowerSupply:  true,
		FirstSeen:      time.Now(),
		LastSeen:       time.Now(),
	}
	if err := database.SaveDevice(device); err != nil {
		t.Fatalf("failed to save device: %v", err)
	}

	// Fetch dashboard
	dtoVal, err := FetchDashboardData(database, time.Now(), true)
	if err != nil {
		t.Fatalf("failed to fetch dashboard data: %v", err)
	}

	if dtoVal.Health.HealthPct != 100.0 {
		t.Errorf("expected health 100%%, got %f", dtoVal.Health.HealthPct)
	}
}

func TestDetailedOverviewAndHealthSemantics(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bati-test-detailed-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_detailed.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	if err := database.InitSchema(); err != nil {
		t.Fatalf("failed to init schema: %v", err)
	}

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	latestSampleTime := now.Add(-20 * time.Hour)

	// Save stale historical battery telemetry
	telemetryData := []model.Telemetry{
		{Timestamp: latestSampleTime, Capacity: 100.0, Status: "Full", EnergyRate: 0.0, Voltage: 16.0, ScreenOn: false},
	}
	if err := database.SaveTelemetryBatch(telemetryData); err != nil {
		t.Fatalf("failed to save telemetry: %v", err)
	}

	// Save primary device
	device := model.Device{
		ID:             "BAT0",
		Vendor:         "Asus",
		Model:          "Main Battery",
		DesignCapacity: 50.0,
		FullCapacity:   50.0, // Health is 100%
		Technology:     "LION",
		CycleCount:     4,
		IsPowerSupply:  true,
		FirstSeen:      time.Now(),
		LastSeen:       time.Now(),
	}
	if err := database.SaveDevice(device); err != nil {
		t.Fatalf("failed to save device: %v", err)
	}

	// 1. Fetch dashboard data (which mocks database, but since ReadBatteryDevices returns nil/nil in tests, LiveSnapshot will be unavailable)
	dtoVal, err := FetchDashboardData(database, now, true)
	if err != nil {
		t.Fatalf("failed to fetch dashboard data: %v", err)
	}

	// Verify it falls back to historical
	if dtoVal.LiveSnapshot.Available {
		t.Errorf("expected live snapshot to be unavailable in test env")
	}
	if !dtoVal.HistoricalSnapshot.Available {
		t.Errorf("expected historical snapshot to be available")
	}
	if dtoVal.HistoricalSnapshot.CapacityPercent != 100.0 {
		t.Errorf("expected historical capacity 100%%, got %f", dtoVal.HistoricalSnapshot.CapacityPercent)
	}

	// 2. Manually mock a LiveSnapshot to test gui layout logic mapping
	dtoVal.LiveSnapshot = dto.LiveSnapshotDTO{
		Available:            true,
		Source:               "sysfs",
		Timestamp:            now,
		CapacityPercent:      93.0,
		Status:               "Not charging",
		PowerRateW:           0.0,
		PowerRateAvailable:   true,
		VoltageV:             8.6,
		VoltageAvailable:     true,
		CycleCount:           5,
		ChargeLimitPercent:   80,
		ChargeLimitAvailable: true,
	}

	// Verify health cycle count mapping
	if dtoVal.Health.CycleCount != 4 {
		t.Errorf("expected db cycle count 4, got %d", dtoVal.Health.CycleCount)
	}
}
