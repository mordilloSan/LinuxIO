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
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
)

const defaultCacheTimeMs = uint16(60000)

func TestPruneBaselinesDropsRemovedContainersAcrossCacheKeys(t *testing.T) {
	at := time.Now()
	dm := &Manager{
		validIds:            map[string]struct{}{"keep": {}},
		lastCpuContainer:    map[uint16]map[string]uint64{1: {"keep": 1, "gone": 2}, 2: {"gone": 3}},
		lastCpuSystem:       map[uint16]map[string]uint64{1: {"keep": 1, "gone": 2}},
		lastNetworkReadTime: map[uint16]map[string]time.Time{1: {"keep": at, "gone": at}},
	}

	dm.pruneBaselinesLocked()

	assert.Equal(t, map[uint16]map[string]uint64{1: {"keep": 1}, 2: {}}, dm.lastCpuContainer)
	assert.Equal(t, map[uint16]map[string]uint64{1: {"keep": 1}}, dm.lastCpuSystem)
	assert.Equal(t, map[uint16]map[string]time.Time{1: {"keep": at}}, dm.lastNetworkReadTime)
}

func testManager(t *testing.T, server *httptest.Server, negotiate bool) *Manager {
	t.Helper()
	dm := &Manager{
		containerStatsMap:   make(map[string]*containermodel.Stats),
		lastCpuContainer:    make(map[uint16]map[string]uint64),
		lastCpuSystem:       make(map[uint16]map[string]uint64),
		networkSentTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		networkRecvTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		lastNetworkReadTime: make(map[uint16]map[string]time.Time),
	}
	opts := []mobyclient.Opt{mobyclient.WithHTTPClient(server.Client()), mobyclient.WithHost(server.URL), mobyclient.WithHTTPResponseHook(func(resp *http.Response) {
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
	assert.Equal(t, []containermodel.Identity{{ID: "0123456789ab", FullID: "0123456789abcdef"}}, ids)
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

func TestStatsPipelineSkipsContainersThatAreNotRunning(t *testing.T) {
	var statsCalls atomic.Int32
	fixtureData := readFixture(t, "container.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.51/containers/json":
			fmt.Fprint(w, `[{"Id":"0000000000010001","Names":["/up"],"State":"running"},{"Id":"0000000000020002","Names":["/looping"],"State":"restarting"},{"Id":"0000000000030003","Names":["/frozen"],"State":"paused"}]`)
		case "/v1.51/containers/0000000000010001/stats", "/v1.51/containers/0000000000030003/stats":
			statsCalls.Add(1)
			_, _ = w.Write(fixtureData)
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	dm.goodDockerVersion, dm.dockerVersionChecked = true, true
	stats, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	names := make([]string, 0, len(stats))
	for _, stat := range stats {
		names = append(names, stat.Name)
	}
	assert.ElementsMatch(t, []string{"up", "frozen"}, names)
	assert.EqualValues(t, 2, statsCalls.Load())
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
	assert.Equal(t, maxStatsAttempts, statsCalls)
}

// A container that stops between the list and its stats read answers with
// empty memory figures; it must be left out, not drawn as zeros, while the
// container that answers stays in the sample.
func TestStatsLeavesOutContainerWhoseReadsKeepFailing(t *testing.T) {
	fixtureData := readFixture(t, "container.json")
	var goneCalls, flakyCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.51/containers/json":
			fmt.Fprint(w, `[{"Id":"0000000000010001","Names":["/gone"],"State":"running"},{"Id":"0000000000020002","Names":["/flaky"],"State":"running"}]`)
		case "/v1.51/containers/0000000000010001/stats":
			goneCalls.Add(1)
			fmt.Fprint(w, `{"memory_stats":{}}`)
		case "/v1.51/containers/0000000000020002/stats":
			if flakyCalls.Add(1) < 3 {
				fmt.Fprint(w, `{"memory_stats":{}}`)
				return
			}
			_, _ = w.Write(fixtureData)
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	dm.goodDockerVersion, dm.dockerVersionChecked = true, true

	stats, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, "flaky", stats[0].Name)
	assert.Positive(t, stats[0].Mem)
	assert.EqualValues(t, maxStatsAttempts, goneCalls.Load())
	assert.EqualValues(t, 3, flakyCalls.Load())
}

// A daemon that lists its containers and then stops answering ends the pass at
// its deadline with the containers that answered, instead of holding the
// collection lock for a request timeout per container and try.
func TestStatsPassEndsAtItsDeadline(t *testing.T) {
	fixtureData := readFixture(t, "container.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.51/containers/json":
			fmt.Fprint(w, `[{"Id":"0000000000010001","Names":["/answers"],"State":"running"},{"Id":"0000000000020002","Names":["/hangs"],"State":"running"}]`)
		case "/v1.51/containers/0000000000010001/stats":
			_, _ = w.Write(fixtureData)
		case "/v1.51/containers/0000000000020002/stats":
			<-r.Context().Done()
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	dm.goodDockerVersion, dm.dockerVersionChecked = true, true
	dm.passTimeout = 200 * time.Millisecond

	start := time.Now()
	stats, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), time.Second)
	require.Len(t, stats, 1)
	assert.Equal(t, "answers", stats[0].Name)
}

// Podman run by root listens at /run/podman/podman.sock (podman.socket), which
// the daemon, running as root, must find after Docker's socket.
func TestDockerSocketsIncludeRootPodman(t *testing.T) {
	socks := dockerSockets()
	require.GreaterOrEqual(t, len(socks), 2)
	assert.Equal(t, []string{"/var/run/docker.sock", "/run/podman/podman.sock"}, socks[:2])
}

// A container still waiting for one of the five request slots when the pass
// ends is left out too, not reported with the previous pass's figures.
func TestStatsPassLeavesOutContainersNeverAsked(t *testing.T) {
	fixtureData := readFixture(t, "container.json")
	var hang atomic.Bool
	var list strings.Builder
	list.WriteString("[")
	for i := 1; i <= 7; i++ {
		if i > 1 {
			list.WriteString(",")
		}
		fmt.Fprintf(&list, `{"Id":"00000000000%d000%d","Names":["/c%d"],"State":"running"}`, i, i, i)
	}
	list.WriteString("]")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.51/containers/json":
			fmt.Fprint(w, list.String())
		case strings.HasSuffix(r.URL.Path, "/stats"):
			if hang.Load() {
				<-r.Context().Done()
				return
			}
			_, _ = w.Write(fixtureData)
		default:
			unexpectedRequest(t, w, r)
		}
	}))
	defer server.Close()
	dm := testManager(t, server, false)
	dm.goodDockerVersion, dm.dockerVersionChecked = true, true
	dm.passTimeout = 200 * time.Millisecond

	stats, err := dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	require.Len(t, stats, 7)

	hang.Store(true)
	stats, err = dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.Empty(t, stats, "no container answered this pass")
}

func TestStatsPassTimeoutFollowsDockerTimeout(t *testing.T) {
	assert.Equal(t, 5*time.Second, statsPassTimeout(2100*time.Millisecond))
	assert.Equal(t, 20*time.Second, statsPassTimeout(10*time.Second))
}

// During a container's first minute its baselines are dropped once, on the
// first pass that sees "Up ... seconds", so the second pass of that minute
// already has a CPU figure; a later restart drops them again.
func TestStatsResetsBaselinesOnceAfterStart(t *testing.T) {
	firstFixture := readFixture(t, "container.json")
	secondFixture := readFixture(t, "container2.json")
	var status atomic.Value
	status.Store("Up 5 seconds")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.51/containers/json":
			fmt.Fprintf(w, `[{"Id":"0123456789abcdef","Names":["/demo"],"State":"running","Status":%q}]`, status.Load())
		case "/v1.51/containers/0123456789abcdef/stats":
			if calls.Add(1)%2 == 1 {
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
	assert.Zero(t, got[0].Cpu)
	status.Store("Up 35 seconds")
	got, err = dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.InDelta(t, 0.14, got[0].Cpu, 0.001)

	status.Store("Up 2 minutes")
	_, err = dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	assert.NotContains(t, dm.startReset, "0123456789ab")

	status.Store("Up 1 second")
	got, err = dm.GetStats(context.Background(), defaultCacheTimeMs)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Zero(t, got[0].Cpu)
	assert.Contains(t, dm.startReset, "0123456789ab")
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
	assert.Equal(t, containermodel.DockerHealthHealthy, stats[0].Health)
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
