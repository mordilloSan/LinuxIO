package app

import (
	"context"
	"encoding/json"
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
