package docker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mobyclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/deltatracker"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
)

const defaultCacheTimeMs = uint16(60000)

func testManager(t *testing.T, server *httptest.Server, negotiate bool) *Manager {
	t.Helper()
	dm := &Manager{
		containerStatsMap:   make(map[string]*container.Stats),
		lastCpuContainer:    make(map[uint16]map[string]uint64),
		lastCpuSystem:       make(map[uint16]map[string]uint64),
		networkSentTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		networkRecvTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		lastNetworkReadTime: make(map[uint16]map[string]time.Time),
	}
	opts := []mobyclient.Opt{mobyclient.WithHTTPClient(server.Client()), mobyclient.WithHost(server.URL), mobyclient.WithResponseHook(func(resp *http.Response) {
		if resp != nil && detectPodmanFromHeader(resp.Header.Get("Server")) {
			dm.libpodDetected.Store(true)
		}
	}), mobyclient.WithTimeout(time.Second)}
	if !negotiate {
		opts = append(opts, mobyclient.WithAPIVersion("1.51"))
	}
	var err error
	dm.client, err = mobyclient.New(opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dm.client.Close() })
	return dm
}

func unexpectedRequest(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	t.Errorf("unexpected request %s", r.URL.RequestURI())
	http.Error(w, "unexpected request", http.StatusInternalServerError)
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return data
}

func TestTypedStatsPipelinePreservesFixtureMath(t *testing.T) {
	firstFixture := readFixture(t, "container.json")
	secondFixture := readFixture(t, "container2.json")
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.51/containers/json":
			fmt.Fprint(w, `[{"Id":"0123456789abcdef","Names":["/demo"],"Image":"demo:latest","State":"running","Status":"Up 2 minutes","Ports":[{"IP":"0.0.0.0","PublicPort":80}]}]`)
		case "/v1.51/containers/0123456789abcdef/stats":
			calls++
			if r.URL.Query().Get("stream") != "false" || r.URL.Query().Get("one-shot") != "true" {
				t.Errorf("stats request missing one-shot options: %s", r.URL.RawQuery)
			}
			if calls == 1 {
				_, _ = w.Write(firstFixture)
			} else {
				_, _ = w.Write(secondFixture)
			}
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	dm.goodDockerVersion, dm.dockerVersionChecked = true, true
	got, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "demo", got[0].Name)
	assert.Equal(t, "80", got[0].Ports)
	assert.InDelta(t, 326.41, got[0].Mem, 1e-9)
	assert.Zero(t, got[0].Cpu)
	got, err = dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.InDelta(t, 0.14, got[0].Cpu, 0.001)
}

func TestTypedHostInfo(t *testing.T) {
	data := readFixture(t, "system_info.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	defer server.Close()
	info, err := testManager(t, server, false).GetHostInfo(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Ubuntu 24.04 LTS", info.OperatingSystem)
	assert.Equal(t, int64(2095882240), info.MemTotal)
}

func TestPodmanHeaderAndNegotiation(t *testing.T) {
	var pings int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			pings++
			w.Header().Set("Api-Version", "1.51")
			w.Header().Set("Server", "Libpod/5.5.0")
			return
		}
		if r.URL.Path == "/v1.51/containers/json" {
			w.Header().Set("Server", "Libpod/5.5.0")
			fmt.Fprint(w, `[]`)
			return
		}
		unexpectedRequest(t, w, r)
	}))
	defer server.Close()
	dm := testManager(t, server, true)
	got, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.True(t, dm.IsPodman())
	assert.Equal(t, 1, pings)
}

func TestTypedListEmptyNamesAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"Id":"0123456789abcdef","Names":[],"State":"running"}]`)
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	ids, err := dm.GetContainerIdentities(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []container.Identity{{ID: "0123456789ab", FullID: "0123456789abcdef"}}, ids)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = dm.GetContainerIdentities(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestVersionProbeRetriesUntilSuccess(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.51/containers/json" {
			fmt.Fprint(w, `[]`)
			return
		}
		if r.URL.Path != "/v1.51/version" {
			unexpectedRequest(t, w, r)
			return
		}
		requests++
		if requests == 1 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"Version":"25.1.0"}`)
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	ok, err := dm.checkDockerVersion(context.Background())
	assert.False(t, ok)
	require.Error(t, err)
	assert.False(t, dm.dockerVersionChecked)
	_, err = dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.True(t, dm.dockerVersionChecked)
	assert.True(t, dm.goodDockerVersion)
	assert.Equal(t, 2, requests)
}

func TestStatsConcurrencyLimitDependsOnEngineVersion(t *testing.T) {
	fixtureData := readFixture(t, "container.json")
	var active, maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.51/containers/json":
			fmt.Fprint(w, `[{"Id":"0000000000010001","Names":["/one"],"State":"running"},{"Id":"0000000000020002","Names":["/two"],"State":"running"},{"Id":"0000000000030003","Names":["/three"],"State":"running"},{"Id":"0000000000040004","Names":["/four"],"State":"running"},{"Id":"0000000000050005","Names":["/five"],"State":"running"},{"Id":"0000000000060006","Names":["/six"],"State":"running"},{"Id":"0000000000070007","Names":["/seven"],"State":"running"},{"Id":"0000000000080008","Names":["/eight"],"State":"running"}]`)
		case strings.HasPrefix(r.URL.Path, "/v1.51/containers/") && strings.HasSuffix(r.URL.Path, "/stats"):
			current := active.Add(1)
			for {
				old := maximum.Load()
				if current <= old || maximum.CompareAndSwap(old, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write(fixtureData)
			active.Add(-1)
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()

	modern := testManager(t, server, false)
	modern.goodDockerVersion, modern.dockerVersionChecked = true, true
	_, err := modern.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.LessOrEqual(t, maximum.Load(), int32(5))

	maximum.Store(0)
	legacy := testManager(t, server, false)
	legacy.goodDockerVersion, legacy.dockerVersionChecked = false, true
	_, err = legacy.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.Greater(t, maximum.Load(), int32(5))
}

func TestStatsPipelineExcludesContainers(t *testing.T) {
	var statsCalls int
	fixtureData := readFixture(t, "container.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.51/containers/json":
			fmt.Fprint(w, `[{"Id":"0000000000010001","Names":["/keep"],"State":"running"},{"Id":"0000000000020002","Names":["/skip"],"State":"running"}]`)
		case "/v1.51/containers/0000000000010001/stats":
			statsCalls++
			_, _ = w.Write(fixtureData)
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	dm.goodDockerVersion, dm.dockerVersionChecked = true, true
	dm.excludeContainers = []string{"skip"}
	stats, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, "keep", stats[0].Name)
	assert.Equal(t, 1, statsCalls)
}

func TestStatsErrorIsRetriedAndDoesNotBreakCollection(t *testing.T) {
	var statsCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.51/containers/json":
			fmt.Fprint(w, `[{"Id":"0000000000010001","Names":["/broken"],"State":"running"}]`)
		case "/v1.51/containers/0000000000010001/stats":
			statsCalls++
			http.Error(w, "stats unavailable", http.StatusServiceUnavailable)
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	dm.goodDockerVersion, dm.dockerVersionChecked = true, true
	stats, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.Empty(t, stats)
	assert.Equal(t, 2, statsCalls)
}

func TestPodmanEmptyHealthUsesInspectFallback(t *testing.T) {
	fixtureData := readFixture(t, "container.json")
	var inspectCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.51/containers/json":
			fmt.Fprint(w, `[{"Id":"0000000000010001","Names":["/pod"],"State":"running","Health":{}}]`)
		case "/v1.51/containers/0000000000010001/stats":
			_, _ = w.Write(fixtureData)
		case "/v1.51/containers/0000000000010001/json":
			inspectCalls++
			fmt.Fprint(w, `{"State":{"Health":{"Status":"healthy"}}}`)
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	dm.goodDockerVersion, dm.dockerVersionChecked = true, true
	dm.setIsPodman()
	stats, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, container.DockerHealthHealthy, stats[0].Health)
	assert.Equal(t, 1, inspectCalls)
}

func TestStatsContextTimeoutPropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := dm.GetContainerIdentities(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestNewManagerDisablesDockerOnBadEnvironment(t *testing.T) {
	cases := map[string]map[string]string{
		"unparsable host":     {"DOCKER_HOST": "://bad"},
		"unsupported scheme":  {"DOCKER_HOST": "ftp://docker.example"},
		"https not supported": {"DOCKER_HOST": "https://docker.example:2376"},
		"unparsable timeout":  {"DOCKER_HOST": "unix:///nonexistent/docker.sock", "DOCKER_TIMEOUT": "soon"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			for key, value := range env {
				t.Setenv(key, value)
			}
			assert.Nil(t, NewManager(context.Background(), nil))
		})
	}
}
