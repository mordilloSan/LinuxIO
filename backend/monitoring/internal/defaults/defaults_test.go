package defaults

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntervalDefaults(t *testing.T) {
	require.Equal(t, time.Minute, CollectorInterval)
	require.Equal(t, time.Hour, SmartRefreshInterval)
}
