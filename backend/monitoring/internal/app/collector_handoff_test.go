package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	procmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
)

func TestCollectorRunHandsSampleToExistingWaitersOnly(t *testing.T) {
	agent := &App{}
	run := agent.beginCollectorRun()
	sample := &collectorAPISample{capturedAt: 123, payloads: map[string]json.RawMessage{}}

	type result struct {
		sample *collectorAPISample
		reused bool
		err    error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			got, reused, err := awaitCollectorRun(context.Background(), run)
			results <- result{sample: got, reused: reused, err: err}
		}()
	}

	agent.finishCollectorRun(run, sample, nil)
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		assert.True(t, got.reused)
		assert.Same(t, sample, got.sample)
	}

	lateSample, reused, err := agent.awaitCollectorSample(context.Background())
	require.NoError(t, err)
	assert.False(t, reused)
	assert.Nil(t, lateSample)
}

func TestCollectorRunCancellationAndFailureDoNotPublish(t *testing.T) {
	agent := &App{}
	run := agent.beginCollectorRun()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sample, reused, err := awaitCollectorRun(ctx, run)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, reused)
	assert.Nil(t, sample)

	collectorErr := errors.New("collector failed")
	agent.finishCollectorRun(run, nil, collectorErr)
	sample, reused, err = awaitCollectorRun(context.Background(), run)
	require.NoError(t, err)
	assert.False(t, reused)
	assert.Nil(t, sample)
}

func TestCollectorAPISampleCoversEveryNonSmartPluginAndIsDetached(t *testing.T) {
	data := &system.CombinedData{
		Stats: system.Stats{
			Cpu:          42,
			Temperatures: map[string]float64{"cpu": 40},
		},
		Details: &system.Details{Hostname: "collector-host"},
		ProcessCount: &procmodel.Count{
			Total: 1,
		},
		Processes: []procmodel.Process{{PID: 7, Name: "worker"}},
		Programs:  []procmodel.Program{{Name: "worker", Count: 1}},
	}

	sample, err := newCollectorAPISample(456, data)
	require.NoError(t, err)
	assert.Equal(t, int64(456), sample.capturedAt)
	for _, plugin := range store.PluginNames() {
		_, exists := sample.pluginPayload(plugin)
		if plugin == store.PluginSmart {
			assert.False(t, exists, "SMART is not part of the collector sample")
		} else {
			assert.True(t, exists, "missing collector payload for %s", plugin)
		}
	}

	processRaw, exists := sample.pluginPayload(store.PluginProcesses)
	require.True(t, exists)
	assert.JSONEq(t, `{"count":{"total":1,"running":0,"sleeping":0,"thread":0},"items":[{"pid":7,"name":"worker","cpu_percent":0,"memory_percent":0,"memory_info":{"rss":0,"vms":0},"io_counters":{}}]}`, string(processRaw))

	data.Stats.Cpu = 99
	data.Stats.Temperatures["cpu"] = 99
	cpuRaw, exists := sample.pluginPayload(store.PluginCPU)
	require.True(t, exists)
	assert.JSONEq(t, `{"cpu_percent":42}`, string(cpuRaw))
	summary, err := sample.systemSummary()
	require.NoError(t, err)
	assert.Equal(t, "collector-host", summary.Hostname)
	assert.InDelta(t, 40, summary.Temperatures["cpu"], 0.001)
}
