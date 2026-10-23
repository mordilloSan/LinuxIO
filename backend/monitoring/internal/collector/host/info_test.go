package host

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestUpdateSystemDetailsMarksDetailsDirty(t *testing.T) {
	m := &Manager{}

	m.UpdateDetails(func(details *systemmodel.Details) {
		details.Hostname = "updated-host"
		details.Podman = true
	})

	assert.True(t, m.detailsDirty)
	assert.Equal(t, "updated-host", m.Details.Hostname)
	assert.True(t, m.Details.Podman)

	original := &systemmodel.CombinedData{}
	realTimeResponse := m.AttachDetails(original, 1000, true)
	assert.Same(t, original, realTimeResponse)
	assert.Nil(t, realTimeResponse.Details)
	assert.True(t, m.detailsDirty)

	response := m.AttachDetails(original, collector.TickKey, false)
	require.NotNil(t, response.Details)
	assert.NotSame(t, original, response)
	assert.Equal(t, "updated-host", response.Details.Hostname)
	assert.True(t, response.Details.Podman)
	assert.False(t, m.detailsDirty)
	assert.Nil(t, original.Details)
}
