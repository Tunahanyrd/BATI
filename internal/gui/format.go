package gui

import (
	"fmt"
	"strings"
	"time"

	"bati/internal/dto"
)

const staleSampleThreshold = 10 * time.Minute

type freshnessView struct {
	Title  string
	Detail string
	Stale  bool
}

func formatDuration(value time.Duration) string {
	if value <= 0 {
		return "0m"
	}
	if value < time.Minute {
		return "<1m"
	}
	rounded := value.Round(time.Minute)
	hours := int(rounded / time.Hour)
	minutes := int((rounded % time.Hour) / time.Minute)
	if hours > 0 {
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func formatRangeDuration(duration time.Duration) string {
	if duration >= 24*time.Hour && duration%(24*time.Hour) == 0 {
		return fmt.Sprintf("%.0fd", duration.Hours()/24)
	}
	return formatDuration(duration)
}

func formatAxisDuration(duration time.Duration) string {
	if duration >= time.Hour {
		if duration%time.Hour == 0 {
			return fmt.Sprintf("%.0fh", duration.Hours())
		}
		return fmt.Sprintf("%.1fh", duration.Hours())
	}
	return fmt.Sprintf("%.0fm", duration.Minutes())
}

func availableText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "unknown") || value == "-" {
		return "Unknown"
	}
	return value
}

func formatCapacity(value float64) string {
	if value <= 0 {
		return "Unknown"
	}
	return fmt.Sprintf("%.2f Wh", value)
}

func rangeLabel(use24h bool, stale bool) string {
	if use24h {
		if stale {
			return "last recorded 24h"
		}
		return "last 24 hours"
	}
	if stale {
		return "last recorded 10d"
	}
	return "last 10 days"
}

func overnightDisplay(overnight dto.OvernightDrainDTO) (string, string) {
	if !overnightAvailable(overnight) {
		return "Not enough data", "Needs a 4h+ sleep or idle period"
	}
	detail := formatDuration(overnight.Duration)
	if provenance := normalizedProvenance(overnight.Confidence); provenance != "" {
		detail += " · " + provenance
	}
	return fmt.Sprintf("%+.1f%%", overnight.DrainPct), detail
}

func overnightAvailable(overnight dto.OvernightDrainDTO) bool {
	return overnight.HasReport && overnight.Duration >= 4*time.Hour
}

func normalizedProvenance(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "observed":
		return "observed"
	case "inferred":
		return "inferred"
	case "estimated":
		return "estimated"
	default:
		return ""
	}
}

func sampleFreshness(latest, now time.Time) freshnessView {
	if latest.IsZero() {
		return freshnessView{
			Title:  "No samples yet",
			Detail: "Run batictl status",
			Stale:  true,
		}
	}
	age := now.Sub(latest)
	if age < 0 {
		age = 0
	}
	ageText := formatAge(age)
	if age >= staleSampleThreshold {
		return freshnessView{
			Title:  "Last sample " + ageText + " ago",
			Detail: "Run batictl status",
			Stale:  true,
		}
	}
	return freshnessView{
		Title:  "Last sample " + ageText + " ago",
		Detail: "History is updating",
	}
}

func formatAge(age time.Duration) string {
	switch {
	case age < 5*time.Second:
		return "just now"
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age/time.Second))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
}

func healthInterpretation(health dto.DeviceHealthDTO) string {
	if !health.HealthAvailable || health.DesignCapacity <= 0 || health.FullCapacity <= 0 {
		return "Capacity data is unavailable."
	}
	base := ""
	switch {
	case health.HealthPct >= 98:
		base = "Full capacity matches design capacity."
	case health.HealthPct >= 80:
		base = "Normal battery wear."
	case health.HealthPct >= 60:
		base = "Battery capacity is reduced."
	default:
		base = "Battery capacity is significantly reduced."
	}
	base += " Health compares full charge capacity with design capacity. Charge limit is a separate charging policy."
	return base
}

func formatRangeSummary(summary dto.RangeSummaryDTO, overnight dto.OvernightDrainDTO, use24h bool, stale bool) string {
	if summary.AvailableDuration <= 0 {
		return "No range summary yet."
	}
	discharge := "0% discharged"
	if summary.TotalDischarge > 0 {
		discharge = fmt.Sprintf("-%.0f%% battery", summary.TotalDischarge)
	}
	parts := []string{
		discharge,
		fmt.Sprintf("+%.0f%% charged", summary.TotalCharge),
		formatDuration(summary.ActiveDuration) + " active",
	}
	if summary.ChargingDuration > 0 {
		parts = append(parts, formatDuration(summary.ChargingDuration)+" charging")
	}
	if summary.SleepDuration > 0 {
		parts = append(parts, formatDuration(summary.SleepDuration)+" sleep")
	}
	if overnightAvailable(overnight) {
		overnightText := fmt.Sprintf("overnight %+.1f%%", overnight.DrainPct)
		if provenance := normalizedProvenance(overnight.Confidence); provenance != "" {
			overnightText += " " + provenance
		}
		parts = append(parts, overnightText)
	} else {
		parts = append(parts, "no overnight session")
	}
	if provenance := normalizedProvenance(summary.Provenance); provenance != "" && provenance != "observed" {
		parts = append(parts, provenance)
	}
	return rangeLabel(use24h, stale) + ": " + strings.Join(parts, " · ")
}
