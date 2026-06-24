package statusfmt

import "strings"

// Display returns a user-facing battery status without changing the raw sysfs/DB value.
func Display(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" || strings.EqualFold(value, "unknown") {
		return "Unknown"
	}
	switch normalize(value) {
	case "discharging":
		return "Discharging..."
	case "not_charging":
		return "Not charging"
	default:
		return value
	}
}

// Lower returns Display in lowercase for compact UI copy.
func Lower(value string) string {
	return strings.ToLower(Display(value))
}

func normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, ".")
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	return value
}
