package csvexport

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bati/internal/dto"
)

var header = []string{
	"timestamp",
	"battery_percent",
	"state",
	"power_rate_w",
	"voltage_v",
	"screen_state",
}

type ExportResult struct {
	Path     string
	RowCount int
}

// WriteTimeline writes real timeline points in a stable, UTC-based CSV format.
func WriteTimeline(writer io.Writer, points []dto.TimelinePointDTO) error {
	output := csv.NewWriter(writer)
	if err := output.Write(header); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for _, point := range points {
		screenState := "inactive"
		if point.ScreenOn {
			screenState = "active"
		}
		record := []string{
			point.Timestamp.UTC().Format(time.RFC3339Nano),
			strconv.FormatFloat(point.Capacity, 'f', 2, 64),
			point.Status,
			strconv.FormatFloat(point.EnergyRate, 'f', 3, 64),
			strconv.FormatFloat(point.Voltage, 'f', 3, 64),
			screenState,
		}
		if err := output.Write(record); err != nil {
			return fmt.Errorf("write csv record: %w", err)
		}
	}
	output.Flush()
	if err := output.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}

// ExportTimeline writes a selected timeline to the user's downloads directory.
func ExportTimeline(points []dto.TimelinePointDTO, use24h bool, now time.Time) (ExportResult, error) {
	directory, err := downloadsDirectory()
	if err != nil {
		return ExportResult{}, err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return ExportResult{}, fmt.Errorf("create export directory: %w", err)
	}
	rangeName := "10d"
	if use24h {
		rangeName = "24h"
	}
	base := fmt.Sprintf("bati-history-%s-%s", rangeName, now.Local().Format("20060102-150405"))
	for suffix := 0; ; suffix++ {
		name := base + ".csv"
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d.csv", base, suffix+1)
		}
		path := filepath.Join(directory, name)
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(openErr) {
			continue
		}
		if openErr != nil {
			return ExportResult{}, fmt.Errorf("create export file: %w", openErr)
		}
		writeErr := WriteTimeline(file, points)
		closeErr := file.Close()
		if writeErr != nil {
			_ = os.Remove(path)
			return ExportResult{}, writeErr
		}
		if closeErr != nil {
			_ = os.Remove(path)
			return ExportResult{}, fmt.Errorf("close export file: %w", closeErr)
		}
		return ExportResult{Path: path, RowCount: len(points)}, nil
	}
}

func downloadsDirectory() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("XDG_DOWNLOAD_DIR")); configured != "" {
		if home, err := os.UserHomeDir(); err == nil {
			configured = strings.ReplaceAll(configured, "$HOME", home)
		}
		return filepath.Clean(configured), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if cwd, cwdErr := os.Getwd(); cwdErr == nil {
			return cwd, nil
		}
		return os.TempDir(), nil
	}
	downloads := filepath.Join(home, "Downloads")
	if stat, statErr := os.Stat(downloads); statErr == nil && stat.IsDir() {
		return downloads, nil
	}
	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		return cwd, nil
	}
	return os.TempDir(), nil
}
