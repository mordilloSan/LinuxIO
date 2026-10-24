package plugins

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHistory(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		got, err := ParseHistory("")
		require.NoError(t, err)
		assert.Equal(t, DefaultHistory(), got)
	})

	t.Run("explicit all", func(t *testing.T) {
		got, err := ParseHistory("all")
		require.NoError(t, err)
		assert.Equal(t, HistoryCapable(), got)
		assert.NotContains(t, got, Processes)
		assert.NotContains(t, got, Programs)
	})

	t.Run("explicit none", func(t *testing.T) {
		got, err := ParseHistory("none")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("invalid plugin", func(t *testing.T) {
		_, err := ParseHistory("cpu,nope")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown history plugin "nope"`)
	})

	t.Run("live-only process plugins", func(t *testing.T) {
		for _, plugin := range []string{Processes, Programs} {
			_, err := ParseHistory(plugin)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "live-only")
		}
	})
}

func TestHistoryEvery(t *testing.T) {
	tick := time.Minute

	t.Run("converts intervals to tick multiples", func(t *testing.T) {
		every, err := HistoryEvery(map[string]time.Duration{
			Containers: 5 * time.Minute,
			CPU:        time.Minute,
		}, tick)
		require.NoError(t, err)
		assert.Equal(t, map[string]uint64{Containers: 5, CPU: 1}, every)
	})

	t.Run("empty map is fine", func(t *testing.T) {
		every, err := HistoryEvery(nil, tick)
		require.NoError(t, err)
		assert.Empty(t, every)
	})

	t.Run("rejects intervals that are not a whole multiple of the tick", func(t *testing.T) {
		_, err := HistoryEvery(map[string]time.Duration{Mem: 90 * time.Second}, tick)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mem")
		assert.Contains(t, err.Error(), "multiple")
	})

	t.Run("rejects intervals below the tick", func(t *testing.T) {
		_, err := HistoryEvery(map[string]time.Duration{Mem: 30 * time.Second}, tick)
		require.Error(t, err)
		_, err = HistoryEvery(map[string]time.Duration{Mem: 0}, tick)
		require.Error(t, err)
	})

	t.Run("rejects plugins without collector history", func(t *testing.T) {
		for _, plugin := range []string{Smart, Processes, Programs, "nope"} {
			_, err := HistoryEvery(map[string]time.Duration{plugin: tick}, tick)
			require.Error(t, err, plugin)
			assert.Contains(t, err.Error(), plugin)
		}
	})
}

func TestLoadDataBatteryUsesNumericArray(t *testing.T) {
	var item LoadData
	require.NoError(t, json.Unmarshal([]byte(`{"battery":[85,1]}`), &item))
	assert.Equal(t, [2]uint16{85, 1}, item.Battery)

	got, err := json.Marshal(item)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"battery":[85,1]`)
}
