package system

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHWSnapshotCacheLoadsOnce(t *testing.T) {
	var cache hwSnapshotCache[int]
	calls := 0
	load := func() (int, error) {
		calls++
		return 42, nil
	}

	for range 3 {
		value, err := cache.get(load)
		require.NoError(t, err)
		assert.Equal(t, 42, value)
	}
	assert.Equal(t, 1, calls, "successful snapshot must be reused")
}

func TestHWSnapshotCacheRetriesAfterError(t *testing.T) {
	var cache hwSnapshotCache[int]
	calls := 0
	load := func() (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("transient failure")
		}
		return 7, nil
	}

	_, err := cache.get(load)
	require.Error(t, err, "first load fails")

	value, err := cache.get(load)
	require.NoError(t, err, "second load retries")
	assert.Equal(t, 7, value)

	_, err = cache.get(load)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "success must stop further loads")
}
