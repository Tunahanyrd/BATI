package gui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"bati/internal/csvexport"
	"bati/internal/dto"
)

func TestSampleFreshness(t *testing.T) {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	fresh := sampleFreshness(now.Add(-12*time.Second), now)
	if fresh.Stale || fresh.Title != "Last sample 12s ago" {
		t.Fatalf("unexpected fresh sample state: %+v", fresh)
	}
	stale := sampleFreshness(now.Add(-10*time.Minute), now)
	if !stale.Stale || stale.Title != "Last sample 10m ago" || stale.Detail != "Run batictl status" {
		t.Fatalf("unexpected stale sample state: %+v", stale)
	}
	empty := sampleFreshness(time.Time{}, now)
	if !empty.Stale || empty.Title != "No samples yet" || empty.Detail != "Run batictl status" {
		t.Fatalf("unexpected no-sample state: %+v", empty)
	}
}

func TestHealthInterpretation(t *testing.T) {
	tests := []struct {
		name   string
		health dto.DeviceHealthDTO
		want   string
	}{
		{
			name: "full",
			health: dto.DeviceHealthDTO{
				HealthAvailable: true, HealthPct: 100, DesignCapacity: 50, FullCapacity: 50,
			},
			want: "Full capacity matches design capacity.",
		},
		{
			name: "normal wear",
			health: dto.DeviceHealthDTO{
				HealthAvailable: true, HealthPct: 85, DesignCapacity: 50, FullCapacity: 42.5,
			},
			want: "Normal battery wear.",
		},
		{name: "missing", health: dto.DeviceHealthDTO{}, want: "Capacity data is unavailable."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := healthInterpretation(test.health)
			if test.name == "missing" {
				if got != test.want {
					t.Fatalf("expected %q, got %q", test.want, got)
				}
			} else {
				if !strings.Contains(got, test.want) {
					t.Fatalf("expected %q to contain %q", got, test.want)
				}
			}
		})
	}
}

func TestRangeSummaryCopy(t *testing.T) {
	summary := dto.RangeSummaryDTO{
		TotalDischarge: 18, TotalCharge: 4, ActiveDuration: 6*time.Hour + 42*time.Minute,
		ChargingDuration: 2 * time.Hour, SleepDuration: 7 * time.Hour,
		AvailableDuration: 24 * time.Hour, Provenance: "observed",
	}
	text := formatRangeSummary(summary, dto.OvernightDrainDTO{
		HasReport: true, Duration: 7 * time.Hour, DrainPct: -4, Confidence: "estimated",
	}, true, false)
	for _, expected := range []string{"last 24 hours", "-18% battery", "+4% charged", "6h42m active", "2h charging", "7h sleep", "overnight -4.0% estimated"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("summary missing %q: %s", expected, text)
		}
	}

	sparse := formatRangeSummary(dto.RangeSummaryDTO{}, dto.OvernightDrainDTO{}, false, false)
	if sparse != "No range summary yet." {
		t.Fatalf("unexpected sparse summary: %q", sparse)
	}

	noSleep := formatRangeSummary(dto.RangeSummaryDTO{
		AvailableDuration: 4 * time.Hour,
		ActiveDuration:    2 * time.Hour,
		Provenance:        "observed",
	}, dto.OvernightDrainDTO{}, true, false)
	if !strings.Contains(noSleep, "no overnight session") {
		t.Fatalf("no-sleep summary must explain overnight status: %q", noSleep)
	}
}

func TestAvailableTextDashFiltering(t *testing.T) {
	if got := availableText("-"); got != "Unknown" {
		t.Fatalf("expected '-' to be formatted to 'Unknown', got %q", got)
	}
	if got := availableText("  -  "); got != "Unknown" {
		t.Fatalf("expected spaces and '-' to format to 'Unknown', got %q", got)
	}
	if got := availableText("BAT0"); got != "BAT0" {
		t.Fatalf("expected standard device to remain unchanged, got %q", got)
	}
}

func TestHealthInterpretationWithLimit(t *testing.T) {
	health := dto.DeviceHealthDTO{
		HealthAvailable:      true,
		HealthPct:            100,
		DesignCapacity:       50,
		FullCapacity:         50,
		ChargeLimitAvailable: true,
		ChargeLimitPercent:   80,
	}
	got := healthInterpretation(health)
	wantSubstring := "Charge limit is a separate charging policy"
	if !strings.Contains(got, wantSubstring) {
		t.Fatalf("expected health interpretation with limit to explain distinction, got %q", got)
	}
}

func TestAboutPrivacyTextIsPlainLinuxSafeCopy(t *testing.T) {
	if strings.Contains(aboutPrivacyText, "🔒") {
		t.Fatalf("about privacy text must not depend on emoji fonts: %q", aboutPrivacyText)
	}
	if strings.Contains(strings.ToLower(aboutPrivacyText), "macos") {
		t.Fatalf("about privacy text must not mention macOS: %q", aboutPrivacyText)
	}
	if !strings.Contains(aboutPrivacyText, "telemetry stays") {
		t.Fatalf("about privacy text lost local-first meaning: %q", aboutPrivacyText)
	}
	if !strings.Contains(aboutSignature, "Tunahanyrd") {
		t.Fatalf("about signature must include the author handle: %q", aboutSignature)
	}
}

func TestTopHeaderStatusShowsLiveAndStaleState(t *testing.T) {
	got := topHeaderStatus(&dto.DashboardDTO{
		LiveSnapshot: dto.LiveSnapshotDTO{
			Available: true, CapacityPercent: 93, Status: "Not charging",
			ChargeLimitAvailable: true, ChargeLimitPercent: 80,
		},
	}, freshnessView{Stale: true})
	for _, expected := range []string{"live 93%", "not charging", "limit 80%", "history stale"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("top header status missing %q: %q", expected, got)
		}
	}
}

func TestTopHeaderStatusFormatsDischargingAsInProgress(t *testing.T) {
	got := topHeaderStatus(&dto.DashboardDTO{
		LiveSnapshot: dto.LiveSnapshotDTO{
			Available: true, CapacityPercent: 71, Status: "Discharging",
		},
	}, freshnessView{})
	if !strings.Contains(got, "discharging...") {
		t.Fatalf("top header status should show discharging as in-progress, got %q", got)
	}
}

func TestExportResultMessageIncludesRowCountAndFailure(t *testing.T) {
	state := &UIState{}
	state.markExportResult(csvexport.ExportResult{Path: "/tmp/bati.csv", RowCount: 2}, nil)
	if state.exportErr || !strings.Contains(state.exportMessage, "Exported 2 rows to /tmp/bati.csv") {
		t.Fatalf("unexpected export success message: err=%t message=%q", state.exportErr, state.exportMessage)
	}

	state.markExportResult(csvexport.ExportResult{}, errors.New("disk full"))
	if !state.exportErr || !strings.Contains(state.exportMessage, "Export failed: disk full") {
		t.Fatalf("unexpected export failure message: err=%t message=%q", state.exportErr, state.exportMessage)
	}

	state.markExportNoData()
	if !state.exportErr || state.exportMessage != "No data to export" {
		t.Fatalf("unexpected no-data export message: err=%t message=%q", state.exportErr, state.exportMessage)
	}
}

func TestSetRangeClearsExportFeedback(t *testing.T) {
	state := &UIState{use24h: true, exportMessage: "Exported 2 rows to /tmp/bati.csv", exportErr: true}
	if !state.setRange(false) {
		t.Fatal("expected range change")
	}
	if state.exportMessage != "" || state.exportErr {
		t.Fatalf("expected range change to clear export feedback, got err=%t message=%q", state.exportErr, state.exportMessage)
	}
}

func TestSessionRowFullColorDiffersFromBatteryLine(t *testing.T) {
	if palette.fullMarker == palette.green {
		t.Fatal("full-session row color must differ from battery level line color")
	}
}
