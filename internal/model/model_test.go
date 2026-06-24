package model

import (
	"math"
	"testing"
)

func TestValidBatteryCapacity(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  bool
	}{
		{name: "empty", value: 0, want: true},
		{name: "normal", value: 85, want: true},
		{name: "full", value: 100, want: true},
		{name: "negative", value: -1, want: false},
		{name: "over percent", value: 100.1, want: false},
		{name: "firmware spike", value: 1693139, want: false},
		{name: "nan", value: math.NaN(), want: false},
		{name: "inf", value: math.Inf(1), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidBatteryCapacity(test.value); got != test.want {
				t.Fatalf("ValidBatteryCapacity(%v)=%t, want %t", test.value, got, test.want)
			}
		})
	}
}
