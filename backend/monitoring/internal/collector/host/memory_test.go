package host

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsedSwapBytes(t *testing.T) {
	tests := []struct {
		name   string
		total  uint64
		free   uint64
		cached uint64
		want   uint64
	}{
		{name: "subtracts free and cached", total: 100, free: 20, cached: 5, want: 75},
		{name: "free greater than total saturates", total: 100, free: 120, cached: 0, want: 0},
		{name: "cached greater than used saturates", total: 100, free: 80, cached: 40, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, usedSwapBytes(tt.total, tt.free, tt.cached))
		})
	}
}
