package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
)

func newLiveCurrentTestApp() *App {
	return &App{
		fsManager:          newFsManager(),
		networkManager:     newNetworkManager(),
		processManager:     newProcessManager(),
		liveProcessManager: newProcessManager(),
		sensorConfig: &SensorConfig{
			skipCollection: true,
			readSem:        make(chan struct{}, 1),
		},
		systemInfoManager: &systemInfoManager{
			systemInfo: system.Info{
				AgentVersion: "test-agent",
				Threads:      2,
			},
			systemDetails: system.Details{
				Hostname:    "live-host",
				OsName:      "Test OS",
				Arch:        "x86_64",
				CpuModel:    "test-cpu",
				Cores:       1,
				Threads:     2,
				MemoryTotal: 16 * 1024 * 1024 * 1024,
			},
		},
	}
}

func TestCurrentPluginGathersEveryPlugin(t *testing.T) {
	agent := newLiveCurrentTestApp()

	for _, plugin := range store.PluginNames() {
		t.Run(plugin, func(t *testing.T) {
			capturedAt, raw, err := agent.CurrentPlugin(context.Background(), plugin)

			require.NoError(t, err)
			assert.NotZero(t, capturedAt)
			assert.NotEmpty(t, raw)
			assert.True(t, json.Valid(raw), "raw plugin payload should be valid JSON: %s", raw)
		})
	}
}

func TestCurrentPluginSpecialPaths(t *testing.T) {
	agent := newLiveCurrentTestApp()

	_, smartRaw, err := agent.CurrentPlugin(context.Background(), store.PluginSmart)
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, string(smartRaw))

	_, _, err = agent.CurrentPlugin(context.Background(), "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown plugin "nope"`)
}

func TestProcessProgramAndContainerTelemetryPluginsCollectFresh(t *testing.T) {
	agent := newLiveCurrentTestApp()

	processCapturedAt, processRaw, err := agent.CurrentPlugin(context.Background(), store.PluginProcesses)
	require.NoError(t, err)
	assert.NotZero(t, processCapturedAt)
	require.True(t, json.Valid(processRaw))

	programCapturedAt, programRaw, err := agent.CurrentPlugin(context.Background(), store.PluginPrograms)
	require.NoError(t, err)
	assert.NotZero(t, programCapturedAt)
	assert.True(t, json.Valid(programRaw))

	telemetryCapturedAt, telemetryRaw, err := agent.CurrentPlugin(context.Background(), store.PluginContainerTelemetry)
	require.NoError(t, err)
	assert.NotZero(t, telemetryCapturedAt)
	assert.JSONEq(t, `[]`, string(telemetryRaw))
}

func TestCurrentPluginsReuseFinishedCollectorProcessSample(t *testing.T) {
	const capturedAt = int64(1_700_000_000_123)
	sample := &collectorAPISample{
		capturedAt: capturedAt,
		payloads: map[string]json.RawMessage{
			store.PluginCPU:       []byte(`{"cpu_percent":17}`),
			store.PluginProcesses: []byte(`{"count":{"total":99},"items":[]}`),
		},
	}
	run := &collectorRun{done: make(chan struct{}), sample: sample}
	close(run.done)
	agent := &App{activeCollectorRun: run}

	gotAt, raw, err := agent.CurrentPlugin(context.Background(), store.PluginProcesses)
	require.NoError(t, err)
	assert.Equal(t, capturedAt, gotAt)
	assert.JSONEq(t, string(sample.payloads[store.PluginProcesses]), string(raw))

	gotAt, payloads, errs := agent.CurrentPlugins(context.Background(), []string{store.PluginCPU, store.PluginProcesses})
	assert.Equal(t, capturedAt, gotAt)
	assert.Empty(t, errs)
	assert.JSONEq(t, string(sample.payloads[store.PluginCPU]), string(payloads[store.PluginCPU]))
	assert.JSONEq(t, string(sample.payloads[store.PluginProcesses]), string(payloads[store.PluginProcesses]))
}

func TestLiveProcessPluginsReuseOneCompletedSample(t *testing.T) {
	at := time.Now()
	run := &liveProcessRun{
		done:       make(chan struct{}),
		capturedAt: at,
		payloads: map[string]json.RawMessage{
			store.PluginProcesses: []byte(`{"count":{"total":1},"items":[]}`),
			store.PluginPrograms:  []byte(`[]`),
		},
	}
	close(run.done)
	agent := &App{liveProcessRun: run}

	first, firstAt, err := agent.currentLiveProcessPayloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, secondAt, err := agent.currentLiveProcessPayloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !firstAt.Equal(at) || !secondAt.Equal(at) {
		t.Fatalf("captured timestamps = %v, %v; want %v", firstAt, secondAt, at)
	}
	first[store.PluginProcesses][0] = 'x'
	if second[store.PluginProcesses][0] != '{' {
		t.Fatal("reused process payload was not detached")
	}
}

func TestLiveProcessPayloadsSharesInFlightSample(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	agent := &App{liveProcessCollect: func(ctx context.Context) (map[string]json.RawMessage, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return map[string]json.RawMessage{store.PluginProcesses: []byte(`[]`)}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}

	results := make(chan error, 5)
	go func() {
		_, _, err := agent.currentLiveProcessPayloads(context.Background())
		results <- err
	}()
	<-started
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			_, _, err := agent.currentLiveProcessPayloads(context.Background())
			results <- err
		})
	}
	close(release)
	wg.Wait()
	for range 5 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("in-flight process sample collected %d times", calls.Load())
	}
}

func TestLiveProcessPayloadsReplacesFailedSample(t *testing.T) {
	var calls atomic.Int32
	agent := &App{liveProcessCollect: func(context.Context) (map[string]json.RawMessage, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("process collection failed")
		}
		return map[string]json.RawMessage{store.PluginProcesses: []byte(`[]`)}, nil
	}}
	if _, _, err := agent.currentLiveProcessPayloads(context.Background()); err == nil {
		t.Fatal("owner must receive process collection failure")
	}
	if _, _, err := agent.currentLiveProcessPayloads(context.Background()); err != nil {
		t.Fatalf("next caller must retry after failure: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("failed process sample was reused or retried excessively: %d calls", calls.Load())
	}
}

func TestSystemSummaryGathersFresh(t *testing.T) {
	agent := newLiveCurrentTestApp()

	capturedAt, summary, err := agent.SystemSummary(context.Background())

	require.NoError(t, err)
	assert.NotZero(t, capturedAt)
	assert.Equal(t, "live-host", summary.Hostname)
	assert.Equal(t, "test-agent", summary.AgentVersion)
	assert.Equal(t, uint64(16*1024*1024*1024), summary.MemoryBytes)
}

func TestCurrentPluginsCollectsOneRequestScopedBatch(t *testing.T) {
	agent := newLiveCurrentTestApp()
	plugins := []string{
		store.PluginCPU,
		store.PluginMem,
		store.PluginContainers,
		store.PluginContainerTelemetry,
		store.PluginSmart,
	}

	capturedAt, payloads, errs := agent.CurrentPlugins(context.Background(), plugins)

	assert.NotZero(t, capturedAt)
	assert.Empty(t, errs)
	for _, plugin := range plugins {
		raw, ok := payloads[plugin]
		require.True(t, ok, "missing %s payload", plugin)
		assert.True(t, json.Valid(raw), "invalid %s payload: %s", plugin, raw)
	}
}

func TestCurrentPluginsReportsCanceledContextForEveryPlugin(t *testing.T) {
	agent := newLiveCurrentTestApp()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plugins := []string{store.PluginCPU, store.PluginMem}

	capturedAt, payloads, errs := agent.CurrentPlugins(ctx, plugins)

	assert.Zero(t, capturedAt)
	assert.Empty(t, payloads)
	for _, plugin := range plugins {
		assert.ErrorIs(t, errs[plugin], context.Canceled)
	}
}

func TestRunCurrentCollectionGroupsRunsConcurrently(t *testing.T) {
	const groupCount = 3
	entered := make(chan struct{}, groupCount)
	release := make(chan struct{})
	done := make(chan struct{})
	groups := make([]func(), 0, groupCount)
	for range groupCount {
		groups = append(groups, func() {
			entered <- struct{}{}
			<-release
		})
	}

	go func() {
		runCurrentCollectionGroups(groups...)
		close(done)
	}()

	for range groupCount {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			<-done
			t.Fatal("collection groups did not start concurrently")
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("collection groups did not finish")
	}
}

func TestLiveSampleKeysAreIndependentFromCollector(t *testing.T) {
	endpoints := append([]string{liveSystemSummaryEndpoint, liveAllEndpoint}, store.PluginNames()...)
	seen := map[uint16]string{collectorDataKeyMs: "collector"}
	for _, endpoint := range endpoints {
		key := liveSampleKey(endpoint)
		if previous, exists := seen[key]; exists {
			t.Fatalf("sample key %d is shared by %s and %s", key, previous, endpoint)
		}
		seen[key] = endpoint
	}
}
