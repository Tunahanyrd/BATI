package diagnostics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"bati/internal/db"
	"bati/internal/statusfmt"
	"bati/internal/sysfs"
)

const staleSampleThreshold = 10 * time.Minute

// ServiceChecker reads the user service state for batid.
type ServiceChecker interface {
	Check(context.Context) ServiceState
}

// ServiceState describes the systemd user unit state.
type ServiceState struct {
	LoadState   string
	ActiveState string
	SubState    string
	Err         string
}

// LiveBattery describes the current sysfs battery state used for diagnostics.
type LiveBattery struct {
	Available            bool
	DeviceID             string
	CapacityPercent      float64
	Status               string
	ChargeLimitAvailable bool
	ChargeLimitPercent   int
	Err                  string
}

// Status is a full diagnostic snapshot of service, database, and live battery state.
type Status struct {
	DBPath                string
	DBExists              bool
	DBSizeBytes           int64
	DBErr                 string
	LatestSampleAvailable bool
	LatestSampleTime      time.Time
	LatestSampleAge       time.Duration
	LatestSampleStale     bool
	TodaySampleCount      int
	TodaySamplesErr       string
	TodayEventCount       int
	TodayEventsErr        string
	Live                  LiveBattery
	Service               ServiceState
	Recommendation        string
}

// SystemdUserChecker checks batid.service through systemctl --user.
type SystemdUserChecker struct {
	Unit    string
	Timeout time.Duration
}

// Snapshot gathers status without mutating service or database state.
func Snapshot(database *db.DB, dbPath string, now time.Time, checker ServiceChecker) Status {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	status := Status{DBPath: dbPath}

	if info, err := os.Stat(dbPath); err == nil {
		status.DBExists = true
		status.DBSizeBytes = info.Size()
	} else if errors.Is(err, os.ErrNotExist) {
		status.DBExists = false
	} else {
		status.DBErr = err.Error()
	}

	status.Service = checkService(checker)
	status.Live = readLiveBattery()

	if database != nil {
		loadDatabaseStatus(database, now, &status)
	}

	status.Recommendation = recommendation(status)
	return status
}

// Check returns the current systemd --user state for the configured unit.
func (checker SystemdUserChecker) Check(parent context.Context) ServiceState {
	unit := checker.Unit
	if unit == "" {
		unit = "batid.service"
	}
	timeout := checker.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		"systemctl",
		"--user",
		"show",
		unit,
		"--property=LoadState,ActiveState,SubState",
		"--no-pager",
	)
	out, err := cmd.CombinedOutput()
	state := parseSystemdShow(out)
	if ctx.Err() != nil {
		state.Err = ctx.Err().Error()
		return state
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		state.Err = msg
	}
	return state
}

func checkService(checker ServiceChecker) ServiceState {
	if checker == nil {
		checker = SystemdUserChecker{}
	}
	return checker.Check(context.Background())
}

func parseSystemdShow(out []byte) ServiceState {
	state := ServiceState{}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			state.LoadState = value
		case "ActiveState":
			state.ActiveState = value
		case "SubState":
			state.SubState = value
		}
	}
	return state
}

func loadDatabaseStatus(database *db.DB, now time.Time, status *Status) {
	latest, err := database.GetLastTelemetryBefore(now)
	switch {
	case err == nil:
		status.LatestSampleAvailable = true
		status.LatestSampleTime = latest.Timestamp
		status.LatestSampleAge = now.Sub(latest.Timestamp)
		if status.LatestSampleAge < 0 {
			status.LatestSampleAge = 0
		}
		status.LatestSampleStale = status.LatestSampleAge >= staleSampleThreshold
	case errors.Is(err, sql.ErrNoRows):
	default:
		status.DBErr = err.Error()
	}

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if points, err := database.GetTelemetryRange(todayStart, now); err == nil {
		status.TodaySampleCount = len(points)
	} else {
		status.TodaySamplesErr = err.Error()
	}
	if events, err := database.GetEventsRange(todayStart, now); err == nil {
		status.TodayEventCount = len(events)
	} else {
		status.TodayEventsErr = err.Error()
	}
}

func readLiveBattery() LiveBattery {
	devices, err := sysfs.ReadBatteryDevices()
	if err != nil {
		return LiveBattery{Err: err.Error()}
	}
	if len(devices) == 0 {
		return LiveBattery{Err: "no battery devices detected"}
	}

	device := devices[0]
	telemetry, err := sysfs.ReadTelemetry(device.ID, false)
	if err != nil {
		return LiveBattery{DeviceID: device.ID, Err: err.Error()}
	}
	return LiveBattery{
		Available:            true,
		DeviceID:             device.ID,
		CapacityPercent:      telemetry.Capacity,
		Status:               telemetry.Status,
		ChargeLimitAvailable: device.ChargeLimitAvailable,
		ChargeLimitPercent:   device.ChargeLimitPercent,
	}
}

func recommendation(status Status) string {
	switch {
	case strings.EqualFold(status.Service.LoadState, "not-found"):
		return "Install and start the user service: ./scripts/install-user-service.sh"
	case status.Service.ActiveState != "" && !strings.EqualFold(status.Service.ActiveState, "active"):
		return "Start the daemon: systemctl --user enable --now batid.service"
	case !status.DBExists:
		return "Start batid to create the local history database."
	case !status.LatestSampleAvailable:
		return "Keep batid running to record the first sample."
	case status.LatestSampleStale:
		return "batid is not recording fresh samples; inspect logs with journalctl --user -u batid.service -n 80."
	default:
		return "Recording looks healthy."
	}
}

func (state ServiceState) Summary() string {
	parts := make([]string, 0, 3)
	if state.LoadState != "" {
		parts = append(parts, "load="+state.LoadState)
	}
	if state.ActiveState != "" {
		parts = append(parts, "active="+state.ActiveState)
	}
	if state.SubState != "" {
		parts = append(parts, "sub="+state.SubState)
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, " ")
}

func (live LiveBattery) Summary() string {
	if !live.Available {
		if live.Err != "" {
			return "unavailable (" + live.Err + ")"
		}
		return "unavailable"
	}
	summary := fmt.Sprintf("%s %.0f%% · %s", live.DeviceID, live.CapacityPercent, statusfmt.Display(live.Status))
	if live.ChargeLimitAvailable && live.ChargeLimitPercent > 0 {
		summary += fmt.Sprintf(" · charge limit %d%%", live.ChargeLimitPercent)
	}
	return summary
}
