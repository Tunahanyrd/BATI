package sysfs

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bati/internal/model"
)

const sysfsPowerPath = "/sys/class/power_supply"

// ReadBatteryDevices scans sysfs to list and read metadata for all batteries supplying power.
func ReadBatteryDevices() ([]model.Device, error) {
	if flag.Lookup("test.v") != nil {
		return nil, nil
	}
	files, err := os.ReadDir(sysfsPowerPath)
	if err != nil {
		return nil, fmt.Errorf("read sysfs power supply dir: %w", err)
	}

	var batteries []model.Device
	for _, f := range files {
		dirPath := filepath.Join(sysfsPowerPath, f.Name())

		// Verify type is Battery
		typeBytes, err := os.ReadFile(filepath.Join(dirPath, "type"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(typeBytes)) != "Battery" {
			continue
		}

		// Verify if it is present
		presentBytes, err := os.ReadFile(filepath.Join(dirPath, "present"))
		if err == nil && strings.TrimSpace(string(presentBytes)) == "0" {
			continue
		}

		// Read metadata
		limit, limitOK, start, _ := ReadChargeLimit(f.Name())
		dev := model.Device{
			ID:                          f.Name(),
			Vendor:                      readStringFile(filepath.Join(dirPath, "manufacturer"), "Unknown"),
			Model:                       readStringFile(filepath.Join(dirPath, "model_name"), "Unknown"),
			Serial:                      readStringFile(filepath.Join(dirPath, "serial_number"), "Unknown"),
			Technology:                  readStringFile(filepath.Join(dirPath, "technology"), "Unknown"),
			IsPowerSupply:               readIntFile(filepath.Join(dirPath, "scope"), 1) != 0, // Scope 1 is System (PowerSupply)
			CycleCount:                  int64(readIntFile(filepath.Join(dirPath, "cycle_count"), 0)),
			FirstSeen:                   time.Now(),
			LastSeen:                    time.Now(),
			ChargeLimitPercent:          limit,
			ChargeLimitAvailable:        limitOK,
			ChargeStartThresholdPercent: start,
		}

		// Capacity Wh (or convert mAh using nominal voltage if energy is missing)
		energyFull := readFloatFile(filepath.Join(dirPath, "energy_full"), 0.0) / 1000000.0 // microWh to Wh
		energyFullDesign := readFloatFile(filepath.Join(dirPath, "energy_full_design"), 0.0) / 1000000.0

		if energyFull == 0.0 {
			// Fallback to charge_full (Ah) * voltage_min_design
			chargeFull := readFloatFile(filepath.Join(dirPath, "charge_full"), 0.0) / 1000000.0 // microAh to Ah
			chargeFullDesign := readFloatFile(filepath.Join(dirPath, "charge_full_design"), 0.0) / 1000000.0
			voltageNominal := readFloatFile(filepath.Join(dirPath, "voltage_min_design"), 0.0) / 1000000.0 // microV to V
			if voltageNominal == 0.0 {
				voltageNominal = readFloatFile(filepath.Join(dirPath, "voltage_now"), 0.0) / 1000000.0
			}
			energyFull = chargeFull * voltageNominal
			energyFullDesign = chargeFullDesign * voltageNominal
		}

		dev.FullCapacity = energyFull
		dev.DesignCapacity = energyFullDesign
		batteries = append(batteries, dev)
	}

	return batteries, nil
}

// ReadTelemetry reads live telemetry metrics for a given battery device (e.g., "BAT0").
func ReadTelemetry(deviceID string, screenOn bool) (model.Telemetry, error) {
	dirPath := filepath.Join(sysfsPowerPath, deviceID)

	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return model.Telemetry{}, fmt.Errorf("device not found: %s", deviceID)
	}

	capacity := readFloatFile(filepath.Join(dirPath, "capacity"), -1.0)
	status := readStringFile(filepath.Join(dirPath, "status"), "Unknown")
	voltage := readFloatFile(filepath.Join(dirPath, "voltage_now"), 0.0) / 1000000.0 // microV to V

	// Calculate energy rate (Watts).
	// power_now is microwatts (uW).
	power := readFloatFile(filepath.Join(dirPath, "power_now"), 0.0) / 1000000.0

	// Fallback: if power_now is not supported, use current_now (uA) * voltage_now (uV)
	if power == 0.0 {
		current := readFloatFile(filepath.Join(dirPath, "current_now"), 0.0) / 1000000.0 // microA to A
		power = current * voltage
	}

	return model.Telemetry{
		Timestamp:  time.Now(),
		Capacity:   capacity,
		Status:     status,
		EnergyRate: power,
		Voltage:    voltage,
		ScreenOn:   screenOn,
	}, nil
}

// Helper file-reading functions
func readStringFile(path string, defaultValue string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultValue
	}
	return strings.TrimSpace(string(b))
}

func readIntFile(path string, defaultValue int) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultValue
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return defaultValue
	}
	return val
}

func readFloatFile(path string, defaultValue float64) float64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return defaultValue
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil {
		return defaultValue
	}
	return val
}

// ReadChargeLimit reads the charge limit thresholds for a battery device.
func ReadChargeLimit(deviceID string) (limit int, limitOK bool, start int, startOK bool) {
	dirPath := filepath.Join(sysfsPowerPath, deviceID)
	// Try charge_control_end_threshold, charge_stop_threshold
	if val := readIntFile(filepath.Join(dirPath, "charge_control_end_threshold"), -1); val >= 0 {
		limit = val
		limitOK = true
	} else if val := readIntFile(filepath.Join(dirPath, "charge_stop_threshold"), -1); val >= 0 {
		limit = val
		limitOK = true
	}
	// Try charge_control_start_threshold, charge_start_threshold
	if val := readIntFile(filepath.Join(dirPath, "charge_control_start_threshold"), -1); val >= 0 {
		start = val
		startOK = true
	} else if val := readIntFile(filepath.Join(dirPath, "charge_start_threshold"), -1); val >= 0 {
		start = val
		startOK = true
	}
	return
}

// ReadCapacityLevel reads the capacity level (e.g., "Normal", "Critical") of the battery.
func ReadCapacityLevel(deviceID string) string {
	dirPath := filepath.Join(sysfsPowerPath, deviceID)
	return readStringFile(filepath.Join(dirPath, "capacity_level"), "Unknown")
}

// ReadEnergyNow reads the current energy remaining in the battery in Wh.
func ReadEnergyNow(deviceID string) float64 {
	dirPath := filepath.Join(sysfsPowerPath, deviceID)
	return readFloatFile(filepath.Join(dirPath, "energy_now"), 0.0) / 1000000.0 // microWh to Wh
}
