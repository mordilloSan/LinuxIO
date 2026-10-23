package units

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTwoDecimals(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"round down", 1.234, 1.23},
		{"round half up", 1.235, 1.24}, // math.Round rounds half up
		{"no rounding needed", 1.23, 1.23},
		{"negative number", -1.235, -1.24}, // math.Round rounds half up (more negative)
		{"zero", 0.0, 0.0},
		{"large number", 123.456, 123.46}, // rounds 5 up
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TwoDecimals(tt.input)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestBytesToMegabytes(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"1 MB", 1048576, 1.0},
		{"512 KB", 524288, 0.5},
		{"zero", 0, 0},
		{"large value", 1073741824, 1024}, // 1 GB = 1024 MB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BytesToMegabytes(tt.input)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestBytesToGigabytes(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected float64
	}{
		{"1 GB", 1073741824, 1.0},
		{"512 MB", 536870912, 0.5},
		{"0 GB", 0, 0},
		{"2 GB", 2147483648, 2.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BytesToGigabytes(tt.input)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}
