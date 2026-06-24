package csvexport

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bati/internal/dto"
)

func TestWriteTimelineSelectedRange(t *testing.T) {
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	points := []dto.TimelinePointDTO{
		{
			Timestamp: start, Capacity: 80, Status: "Not charging, threshold",
			EnergyRate: 0, Voltage: 15.8, ScreenOn: true,
		},
		{
			Timestamp: start.Add(time.Hour), Capacity: 75, Status: "Discharging",
			EnergyRate: 6.25, Voltage: 15.4, ScreenOn: false,
		},
	}
	var output bytes.Buffer
	if err := WriteTimeline(&output, points); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(output.String())).ReadAll()
	if err != nil {
		t.Fatalf("csv output is invalid: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected header and two selected-range rows, got %d", len(records))
	}
	wantHeader := []string{"timestamp", "battery_percent", "state", "power_rate_w", "voltage_v", "screen_state"}
	if strings.Join(records[0], ",") != strings.Join(wantHeader, ",") {
		t.Fatalf("unexpected csv header: %v", records[0])
	}
	if records[1][2] != "Not charging, threshold" {
		t.Fatalf("csv escaping changed status: %q", records[1][2])
	}
	if records[1][5] != "active" || records[2][5] != "inactive" {
		t.Fatalf("screen state formatting is incorrect: %v", records)
	}
	if records[1][0] != "2026-06-22T10:00:00Z" {
		t.Fatalf("timestamp must remain UTC in export, got %q", records[1][0])
	}
}

func TestWriteTimelineEmptyData(t *testing.T) {
	var output bytes.Buffer
	if err := WriteTimeline(&output, nil); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(output.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0]) != len(header) {
		t.Fatalf("empty export should contain only the stable header: %v", records)
	}
}

func TestExportTimelineWritesFileAndReportsRowCount(t *testing.T) {
	downloads := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", downloads)
	now := time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC)
	points := []dto.TimelinePointDTO{
		{Timestamp: now.Add(-time.Hour), Capacity: 93, Status: "Not charging"},
	}

	result, err := ExportTimeline(points, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowCount != 1 {
		t.Fatalf("expected row count 1, got %+v", result)
	}
	if filepath.Dir(result.Path) != downloads {
		t.Fatalf("expected export in XDG download dir %q, got %q", downloads, result.Path)
	}
	content, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(string(content))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header and one row, got %v", records)
	}
}

func TestExportTimelineEmptyWritesHeaderOnlyAndReportsZeroRows(t *testing.T) {
	downloads := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", downloads)
	result, err := ExportTimeline(nil, false, time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if result.RowCount != 0 {
		t.Fatalf("expected zero rows, got %+v", result)
	}
	content, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(string(content))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected header-only export, got %v", records)
	}
}
