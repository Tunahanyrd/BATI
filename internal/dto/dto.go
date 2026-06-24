package dto

import (
	"time"

	"bati/internal/model"
)

// BatteryStatusDTO encapsulates real-time status of the battery.
type BatteryStatusDTO struct {
	Available     bool          `json:"available"`
	Timestamp     time.Time     `json:"timestamp"`
	Capacity      float64       `json:"capacity"`
	Status        string        `json:"status"`
	EnergyRate    float64       `json:"energy_rate"` // in Watts
	Voltage       float64       `json:"voltage"`     // in Volts
	RemainingTime time.Duration `json:"remaining_time"`
	PluggedIn     bool          `json:"plugged_in"`
}

// DeviceHealthDTO aggregates battery health metrics.
type DeviceHealthDTO struct {
	Available                   bool    `json:"available"`
	HealthAvailable             bool    `json:"health_available"`
	CycleCountAvailable         bool    `json:"cycle_count_available"`
	Model                       string  `json:"model"`
	Vendor                      string  `json:"vendor"`
	DesignCapacity              float64 `json:"design_capacity"` // Wh
	FullCapacity                float64 `json:"full_capacity"`   // Wh
	HealthPct                   float64 `json:"health_pct"`      // Health ratio (e.g. 92.4%)
	CycleCount                  int64   `json:"cycle_count"`
	Technology                  string  `json:"technology"`
	ChargeLimitPercent          int     `json:"charge_limit_percent"`
	ChargeLimitAvailable        bool    `json:"charge_limit_available"`
	ChargeStartThresholdPercent int     `json:"charge_start_threshold_percent"`
}

// OvernightDrainDTO represents estimated overnight energy loss.
type OvernightDrainDTO struct {
	HasReport  bool          `json:"has_report"`
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	StartPct   float64       `json:"start_pct"`
	EndPct     float64       `json:"end_pct"`
	DrainPct   float64       `json:"drain_pct"`
	Duration   time.Duration `json:"duration"`
	Type       string        `json:"type"`
	Confidence string        `json:"confidence"`
}

// TimelinePointDTO represents a point on the main battery timeline graph (Upper Chart).
type TimelinePointDTO struct {
	Timestamp  time.Time `json:"timestamp"`
	Capacity   float64   `json:"capacity"`
	Status     string    `json:"status"`
	ScreenOn   bool      `json:"screen_on"`
	EnergyRate float64   `json:"energy_rate"`
	Voltage    float64   `json:"voltage"`
}

// SOTBarDTO represents screen active time grouped by interval (Lower Chart).
type SOTBarDTO struct {
	Start    time.Time     `json:"start"`
	End      time.Time     `json:"end"`
	Label    string        `json:"label"`
	Duration time.Duration `json:"duration"`
	Coverage time.Duration `json:"coverage"`
	Observed bool          `json:"observed"`
	Partial  bool          `json:"partial"`
}

// RangeSummaryDTO contains the selected range's user-facing aggregate values.
type RangeSummaryDTO struct {
	TotalDischarge    float64       `json:"total_discharge"`
	TotalCharge       float64       `json:"total_charge"`
	ActiveDuration    time.Duration `json:"active_duration"`
	ChargingDuration  time.Duration `json:"charging_duration"`
	SleepDuration     time.Duration `json:"sleep_duration"`
	AvailableDuration time.Duration `json:"available_duration"`
	Provenance        string        `json:"provenance"`
}

// DailySummaryDTO contains a local-calendar-day aggregate for the 10-day view.
// Start and End remain absolute timestamps; only grouping and labels use local time.
type DailySummaryDTO struct {
	Start            time.Time     `json:"start"`
	End              time.Time     `json:"end"`
	Label            string        `json:"label"`
	TotalDischarge   float64       `json:"total_discharge"`
	TotalCharge      float64       `json:"total_charge"`
	ActiveDuration   time.Duration `json:"active_duration"`
	ChargingDuration time.Duration `json:"charging_duration"`
	SleepDuration    time.Duration `json:"sleep_duration"`
	Coverage         time.Duration `json:"coverage"`
	Observed         bool          `json:"observed"`
	Partial          bool          `json:"partial"`
	Provenance       string        `json:"provenance"`
}

// TimelineDTO contains all query-layer data needed by both usage charts.
type TimelineDTO struct {
	Start         time.Time          `json:"start"`
	End           time.Time          `json:"end"`
	AvailableFrom time.Time          `json:"available_from"`
	AvailableTo   time.Time          `json:"available_to"`
	Points        []TimelinePointDTO `json:"points"`
	SOTBars       []SOTBarDTO        `json:"sot_bars"`
	Sessions      []model.Session    `json:"sessions"`
	Days          []DailySummaryDTO  `json:"days"`
}

// LastChargeDTO describes the most recent observed full charge.
type LastChargeDTO struct {
	Available bool      `json:"available"`
	Timestamp time.Time `json:"timestamp"`
	Capacity  float64   `json:"capacity"`
}

// LowPowerModeDTO stays unavailable until a supported system source exists.
type LowPowerModeDTO struct {
	Available bool   `json:"available"`
	Value     string `json:"value"`
}

// LiveBatteryDTO represents the current live battery state from sysfs.
type LiveBatteryDTO struct {
	Available  bool      `json:"available"`
	Timestamp  time.Time `json:"timestamp"`
	Capacity   float64   `json:"capacity"`
	Status     string    `json:"status"`
	EnergyRate float64   `json:"energy_rate"`
	Voltage    float64   `json:"voltage"`
}

// LiveSnapshotDTO represents the live current battery reading from sysfs/upower.
type LiveSnapshotDTO struct {
	Available                     bool      `json:"available"`
	Source                        string    `json:"source"` // "sysfs" or "upower"
	Timestamp                     time.Time `json:"timestamp"`
	CapacityPercent               float64   `json:"capacity_percent"`
	CapacityLevel                 string    `json:"capacity_level,omitempty"`
	Status                        string    `json:"status"`
	PowerRateW                    float64   `json:"power_rate_w,omitempty"`
	PowerRateAvailable            bool      `json:"power_rate_available"`
	VoltageV                      float64   `json:"voltage_v,omitempty"`
	VoltageAvailable              bool      `json:"voltage_available"`
	EnergyNowWh                   float64   `json:"energy_now_wh,omitempty"`
	EnergyFullWh                  float64   `json:"energy_full_wh,omitempty"`
	EnergyFullDesignWh            float64   `json:"energy_full_design_wh,omitempty"`
	CycleCount                    int64     `json:"cycle_count,omitempty"`
	Manufacturer                  string    `json:"manufacturer,omitempty"`
	ModelName                     string    `json:"model_name,omitempty"`
	Technology                    string    `json:"technology,omitempty"`
	ChargeLimitPercent            int       `json:"charge_limit_percent,omitempty"`
	ChargeLimitAvailable          bool      `json:"charge_limit_available"`
	ChargeStartThresholdPercent   int       `json:"charge_start_threshold_percent,omitempty"`
	ChargeStartThresholdAvailable bool      `json:"charge_start_threshold_available"`
}

// HistoricalSnapshotDTO represents the latest historical sample from sqlite.
type HistoricalSnapshotDTO struct {
	Available       bool          `json:"available"`
	Timestamp       time.Time     `json:"timestamp"`
	Age             time.Duration `json:"age"`
	Stale           bool          `json:"stale"`
	CapacityPercent float64       `json:"capacity_percent"`
	Status          string        `json:"status"`
	PowerRateW      float64       `json:"power_rate_w"`
	VoltageV        float64       `json:"voltage_v"`
}

// DashboardDTO represents the complete aggregated state required to render the GUI.
type DashboardDTO struct {
	Status             BatteryStatusDTO      `json:"status"`
	Live               LiveBatteryDTO        `json:"live"`
	Health             DeviceHealthDTO       `json:"health"`
	Overnight          OvernightDrainDTO     `json:"overnight"`
	LastCharge         LastChargeDTO         `json:"last_charge"`
	LowPowerMode       LowPowerModeDTO       `json:"low_power_mode"`
	Timeline           TimelineDTO           `json:"timeline"`
	TotalDischarge     float64               `json:"total_discharge"`
	TotalCharge        float64               `json:"total_charge"`
	ActiveDuration     time.Duration         `json:"active_duration"`
	SleepDuration      time.Duration         `json:"sleep_duration"`
	Summary            RangeSummaryDTO       `json:"summary"`
	RecentSummary      RangeSummaryDTO       `json:"recent_summary"`
	LiveSnapshot       LiveSnapshotDTO       `json:"live_snapshot"`
	HistoricalSnapshot HistoricalSnapshotDTO `json:"historical_snapshot"`
}
