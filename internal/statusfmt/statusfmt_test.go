package statusfmt

import "testing"

func TestDisplayStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Discharging", "Discharging..."},
		{"discharging", "Discharging..."},
		{"discharging...", "Discharging..."},
		{"Not charging", "Not charging"},
		{"not_charging", "Not charging"},
		{"", "Unknown"},
		{"-", "Unknown"},
		{"Charging", "Charging"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			if got := Display(test.input); got != test.want {
				t.Fatalf("Display(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestLowerStatus(t *testing.T) {
	if got := Lower("Discharging"); got != "discharging..." {
		t.Fatalf("unexpected lowercase display: %q", got)
	}
}
