package model

import "time"

// Telemetry represents a single battery reading snapshot.
type Telemetry struct {
	Timestamp  time.Time `json:"timestamp"`
	Capacity   float64   `json:"capacity"`    // Battery level percentage (0.0 to 100.0)
	Status     string    `json:"status"`      // e.g. "Charging", "Discharging", "Full", "Unknown"
	EnergyRate float64   `json:"energy_rate"` // Power consumption/charge rate in Watts (W)
	Voltage    float64   `json:"voltage"`     // Battery voltage (V)
	ScreenOn   bool      `json:"screen_on"`   // Calculated/estimated screen activity status
}

// Event represents a system boundary event like sleep, resume, boot, or AC plug change.
type Event struct {
	ID        int64     `json:"id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`    // e.g. "boot", "sleep", "resume", "ac_connected", "ac_disconnected"
	Payload   string    `json:"payload"` // Optional JSON or extra metadata text
}

// Device represents details of the monitored power supply.
type Device struct {
	ID                          string    `json:"id"` // Unique object path or identifier (e.g., /org/freedesktop/UPower/devices/battery_BAT0)
	Vendor                      string    `json:"vendor"`
	Model                       string    `json:"model"`
	Serial                      string    `json:"serial"`
	DesignCapacity              float64   `json:"design_capacity"` // Wh or mAh
	FullCapacity                float64   `json:"full_capacity"`   // Wh or mAh
	Technology                  string    `json:"technology"`
	CycleCount                  int64     `json:"cycle_count"`
	IsPowerSupply               bool      `json:"is_power_supply"`
	FirstSeen                   time.Time `json:"first_seen"`
	LastSeen                    time.Time `json:"last_seen"`
	ChargeLimitPercent          int       `json:"charge_limit_percent"`
	ChargeLimitAvailable        bool      `json:"charge_limit_available"`
	ChargeStartThresholdPercent int       `json:"charge_start_threshold_percent"`
}

// Session represents a segmented block of battery state (charging, discharging, sleeping, full, or not_charging).
type Session struct {
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	Type       string        `json:"type"` // "charging", "discharging", "sleeping", "full", "not_charging", "unknown"
	StartPct   float64       `json:"start_pct"`
	EndPct     float64       `json:"end_pct"`
	DeltaPct   float64       `json:"delta_pct"` // e.g. -12.0 or +30.0
	Duration   time.Duration `json:"duration"`
	Provenance string        `json:"provenance"` // "observed" or "estimated"
}

// DailySummary aggregates metrics for a single calendar day or rolling range.
type DailySummary struct {
	Date           time.Time     `json:"date"`
	TotalDischarge float64       `json:"total_discharge"` // Total percentage dropped (positive value, e.g. 24.0 for -24%)
	TotalCharge    float64       `json:"total_charge"`    // Total percentage gained (e.g. 45.0)
	SleepDuration  time.Duration `json:"sleep_duration"`
	ActiveDuration time.Duration `json:"active_duration"` // Screen-on time
	Sessions       []Session     `json:"sessions"`
	Provenance     string        `json:"provenance"` // "observed" or "estimated"
}
