package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestUpdateSystemDetailsMarksDetailsDirty(t *testing.T) {
	m := &systemInfoManager{}

	m.updateSystemDetails(func(details *system.Details) {
		details.Hostname = "updated-host"
		details.Podman = true
	})

	assert.True(t, m.detailsDirty)
	assert.Equal(t, "updated-host", m.systemDetails.Hostname)
	assert.True(t, m.systemDetails.Podman)

	original := &system.CombinedData{}
	realTimeResponse := m.attachSystemDetails(original, 1000, true)
	assert.Same(t, original, realTimeResponse)
	assert.Nil(t, realTimeResponse.Details)
	assert.True(t, m.detailsDirty)

	response := m.attachSystemDetails(original, collectorDataKeyMs, false)
	require.NotNil(t, response.Details)
	assert.NotSame(t, original, response)
	assert.Equal(t, "updated-host", response.Details.Hostname)
	assert.True(t, response.Details.Podman)
	assert.False(t, m.detailsDirty)
	assert.Nil(t, original.Details)
}

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
