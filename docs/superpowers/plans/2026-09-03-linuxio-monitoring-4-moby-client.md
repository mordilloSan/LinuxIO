# linuxio-monitoring Plan 4: Moby Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the daemon's raw Docker Engine HTTP client and hand-written DTOs with the `github.com/moby/moby/client` the bridge already uses, keeping container statistics identical for identical inputs.

**Architecture:** `internal/integration/docker.Manager` keeps its public surface (`NewManager`, `GetStats`, `GetContainerIdentities`, `GetHostInfo`, `IsPodman`, `ReseedFromCollector`, `SetCollectorKey`) and its delta trackers, exclusion patterns, concurrency limit and Podman detection. Only the transport changes: container listing, one-shot stats, version, info and inspect go through the typed client, and the response types are `github.com/moby/moby/api/types/container` and `types/system`. Parity is proven by feeding the existing fixture JSON through the new code path.

**Tech Stack:** Go 1.27, `github.com/moby/moby/client` v0.5.1, `github.com/moby/moby/api` v1.55.0, `httptest`.

**Spec:** `docs/TODO/linuxio-monitoring.md` ("Final phase, Docker client")

**Depends on:** Plans 1 to 3 landed and green. This phase depends on nothing else and nothing depends on it.

## Global Constraints

- Plan 1's Global Constraints apply.
- No behaviour change in the numbers: CPU percent (`cpuDelta / systemDelta * 100`, zero when either counter went backwards, when the system delta is zero, or when there is no previous container counter), memory (`usage - inactive_file`, falling back to `usage - cache`, rejecting non-positive or absurd values), network rates per key via the delta trackers with the 5 GB/s sanity cap.
- Podman support stays: detection from the server version (`Platform.Name` or any component named `Podman Engine`) and the inspect fallback for health.
- Docker engines before 25 keep the batch-retry path for the one-shot stats bug.
- `DOCKER_HOST`, `DOCKER_TIMEOUT` and `EXCLUDE_CONTAINERS` keep their meaning.
- `internal/integration/docker/dockerapi` is deleted at the end; nothing outside the docker package may import it beforehand (Plan 1 already confirmed only the docker package does).

---

## File Structure

| Path | Change |
|---|---|
| `backend/monitoring/internal/integration/docker/client.go` | New: constructs the moby client from `DOCKER_HOST`/`DOCKER_TIMEOUT`; typed wrappers `listContainers`, `containerStats`, `serverVersion`, `inspectHealth`, `hostInfo`. |
| `backend/monitoring/internal/integration/docker/docker.go` | Uses the wrappers; DTO field access moves to moby types; `get`, `decode`, `buf`, `decoder`, `apiStats`, `userAgentRoundTripper` removed. |
| `backend/monitoring/internal/integration/docker/stats.go` | New: `cpuPercentLinux(stats container.StatsResponse, prevContainer, prevSystem uint64) float64`, `memoryUsage(stats container.StatsResponse) (uint64, error)`. |
| `backend/monitoring/internal/integration/docker/*_test.go` | Test servers answer versioned paths; fixture parity tests decode into moby types. |
| `backend/monitoring/internal/integration/docker/dockerapi/` | Deleted. |
| `backend/monitoring/internal/domain/system/system.go` | `Details.Podman` unchanged; `HostInfo` consumers switch to `system.Info` fields. |

---

### Task 1: Pure formulas on moby types with fixture parity

**Files:**
- Create: `backend/monitoring/internal/integration/docker/stats.go`, `backend/monitoring/internal/integration/docker/stats_test.go`

**Interfaces:**
- Produces:

```go
func cpuPercentLinux(stats *container.StatsResponse, prevContainer, prevSystem uint64) float64
func memoryUsage(stats *container.StatsResponse) (uint64, error)
```

where `container` is `github.com/moby/moby/api/types/container`.

- [ ] **Step 1: Write the parity test against the fixtures**

```go
package docker

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/integration/docker/dockerapi"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFormulasMatchLegacyDTOs(t *testing.T) {
	for _, fixture := range []string{"container.json", "container2.json"} {
		data := loadFixture(t, fixture)
		var legacy dockerapi.Stats
		if err := json.Unmarshal(data, &legacy); err != nil {
			t.Fatal(err)
		}
		var typed container.StatsResponse
		if err := json.Unmarshal(data, &typed); err != nil {
			t.Fatal(err)
		}

		prevContainer := legacy.CPUStats.CPUUsage.TotalUsage / 2
		prevSystem := legacy.CPUStats.SystemUsage / 2
		if got, want := cpuPercentLinux(&typed, prevContainer, prevSystem), legacy.CalculateCPUPercentLinux(prevContainer, prevSystem); got != want {
			t.Fatalf("%s cpu = %v, want %v", fixture, got, want)
		}
		if got, want := cpuPercentLinux(&typed, 0, prevSystem), legacy.CalculateCPUPercentLinux(0, prevSystem); got != want {
			t.Fatalf("%s cpu with no previous = %v, want %v", fixture, got, want)
		}

		gotMem, gotErr := memoryUsage(&typed)
		wantMem, wantErr := calculateMemoryUsage(&legacy)
		if gotMem != wantMem || (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("%s memory = %d/%v, want %d/%v", fixture, gotMem, gotErr, wantMem, wantErr)
		}
	}
}
```

This test imports the legacy package on purpose; it is deleted with the DTOs in Task 4 after the parity is recorded.

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/integration/docker/... GO_TEST_FLAGS='-run TestFormulasMatchLegacyDTOs'
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement `stats.go`**

```go
package docker

import (
	"errors"

	"github.com/moby/moby/api/types/container"
)

// cpuPercentLinux mirrors the Docker CLI formula without the core multiplier:
// the app scales by thread count where it needs the multi-core convention.
func cpuPercentLinux(stats *container.StatsResponse, prevContainer, prevSystem uint64) float64 {
	total := stats.CPUStats.CPUUsage.TotalUsage
	system := stats.CPUStats.SystemUsage
	if total < prevContainer || system < prevSystem {
		return 0
	}
	cpuDelta := total - prevContainer
	systemDelta := system - prevSystem
	if systemDelta == 0 || prevContainer == 0 {
		return 0
	}
	return float64(cpuDelta) / float64(systemDelta) * 100
}

var errBadMemoryStats = errors.New("bad memory stats")

// memoryUsage subtracts the page cache the way `docker stats` does: cgroup v2
// exposes inactive_file, cgroup v1 exposes cache.
func memoryUsage(stats *container.StatsResponse) (uint64, error) {
	cache := stats.MemoryStats.Stats["inactive_file"]
	if cache == 0 {
		cache = stats.MemoryStats.Stats["cache"]
	}
	if cache > stats.MemoryStats.Usage {
		return 0, errBadMemoryStats
	}
	used := stats.MemoryStats.Usage - cache
	if used == 0 || used > maxMemoryUsage {
		return 0, errBadMemoryStats
	}
	return used, nil
}
```

- [ ] **Step 4: Run**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/integration/docker/... GO_TEST_FLAGS='-run TestFormulasMatchLegacyDTOs'
```

Expected: PASS.

- [ ] **Step 5: Stage**

```bash
git add backend/monitoring/internal/integration/docker/stats.go backend/monitoring/internal/integration/docker/stats_test.go
```

---

### Task 2: Typed client wrappers

**Files:**
- Create: `backend/monitoring/internal/integration/docker/client.go`, `backend/monitoring/internal/integration/docker/client_test.go`

**Interfaces:**
- Produces:

```go
type engineClient struct{ cli *client.Client }

func newEngineClient(host string, timeout time.Duration) (*engineClient, error)
func (c *engineClient) listContainers(ctx context.Context, all bool) ([]container.Summary, error)
func (c *engineClient) containerStats(ctx context.Context, id string) (*container.StatsResponse, error) // one-shot, no stream
func (c *engineClient) serverVersion(ctx context.Context) (client.ServerVersionResult, error)
func (c *engineClient) inspectHealth(ctx context.Context, id string) (string, error) // State.Health.Status, "" when absent
func (c *engineClient) hostInfo(ctx context.Context) (system.Info, error)
```

`host` is the `DOCKER_HOST` value or `unix:///var/run/docker.sock`; an empty `DOCKER_HOST` still disables the manager as today.

- [ ] **Step 1: Write the failing test with an httptest engine**

```go
package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newFakeEngine(t *testing.T, routes map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for suffix, payload := range routes {
			if strings.HasSuffix(r.URL.Path, suffix) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(payload)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestEngineClientListsAndReadsStats(t *testing.T) {
	server := newFakeEngine(t, map[string]any{
		"/containers/json": []map[string]any{{"Id": "abc123def456789", "Names": []string{"/web"}, "State": "running", "Status": "Up 2 hours"}},
		"/stats":           json.RawMessage(loadFixture(t, "container.json")),
		"/version":         map[string]any{"Version": "27.0.1", "ApiVersion": "1.46", "Platform": map[string]any{"Name": "Docker Engine - Community"}},
	})
	cli, err := newEngineClient(server.URL, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	items, err := cli.listContainers(context.Background(), true)
	if err != nil || len(items) != 1 || items[0].Names[0] != "/web" {
		t.Fatalf("list = %+v err=%v", items, err)
	}
	stats, err := cli.containerStats(context.Background(), "abc123def456")
	if err != nil || stats.CPUStats.CPUUsage.TotalUsage == 0 {
		t.Fatalf("stats = %+v err=%v", stats, err)
	}
	version, err := cli.serverVersion(context.Background())
	if err != nil || version.Version != "27.0.1" {
		t.Fatalf("version = %+v err=%v", version, err)
	}
}

func TestNewEngineClientRejectsBadHost(t *testing.T) {
	if _, err := newEngineClient("ftp://nope", time.Second); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}
```

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/integration/docker/... GO_TEST_FLAGS='-run TestEngineClient|TestNewEngineClient'
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement `client.go`**

```go
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

type engineClient struct {
	cli *client.Client
}

// newEngineClient wraps the moby client for the daemon. host is a Docker host
// URL (unix://, tcp://, http://, https://); timeout bounds every request.
func newEngineClient(host string, timeout time.Duration) (*engineClient, error) {
	cli, err := client.New(client.WithHost(host), client.WithTimeout(timeout), client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client for %s: %w", host, err)
	}
	return &engineClient{cli: cli}, nil
}

func (c *engineClient) close() error { return c.cli.Close() }

func (c *engineClient) listContainers(ctx context.Context, all bool) ([]container.Summary, error) {
	result, err := c.cli.ContainerList(ctx, client.ContainerListOptions{All: all})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	return result.Items, nil
}

// containerStats reads one sample. Stream and IncludePreviousSample stay off,
// which is the `one-shot=1` request the raw client used to send.
func (c *engineClient) containerStats(ctx context.Context, id string) (*container.StatsResponse, error) {
	result, err := c.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{})
	if err != nil {
		return nil, fmt.Errorf("container %s stats: %w", id, err)
	}
	defer result.Body.Close()
	var stats container.StatsResponse
	if err := json.NewDecoder(io.LimitReader(result.Body, 4<<20)).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode container %s stats: %w", id, err)
	}
	return &stats, nil
}

func (c *engineClient) serverVersion(ctx context.Context) (client.ServerVersionResult, error) {
	result, err := c.cli.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return client.ServerVersionResult{}, fmt.Errorf("server version: %w", err)
	}
	return result, nil
}

func (c *engineClient) inspectHealth(ctx context.Context, id string) (string, error) {
	result, err := c.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect container %s: %w", id, err)
	}
	if result.Container.State == nil || result.Container.State.Health == nil {
		return "", nil
	}
	return string(result.Container.State.Health.Status), nil
}

func (c *engineClient) hostInfo(ctx context.Context) (system.Info, error) {
	result, err := c.cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return system.Info{}, fmt.Errorf("engine info: %w", err)
	}
	return result.Info, nil
}
```

If `client.WithAPIVersionNegotiation` does not exist in v0.5.1, drop it; the client negotiates by default in that line. If `ContainerStatsOptions` has no `Stream` field spelled that way, the zero value is still the one-shot request; verify with:

```bash
rg -n "type ContainerStatsOptions" -A12 "$(cd backend && go env GOMODCACHE)/github.com/moby/moby/client@v0.5.1/container_stats.go"
rg -n "Health|State " "$(cd backend && go env GOMODCACHE)/github.com/moby/moby/api@v1.55.0/types/container/container.go" | head
```

and adjust the two field accesses to the names printed.

- [ ] **Step 4: Run**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/integration/docker/... GO_TEST_FLAGS='-run TestEngineClient|TestNewEngineClient'
```

Expected: PASS.

- [ ] **Step 5: Stage**

```bash
git add backend/monitoring/internal/integration/docker/client.go backend/monitoring/internal/integration/docker/client_test.go
```

---

### Task 3: Manager on the typed client

**Files:**
- Modify: `backend/monitoring/internal/integration/docker/docker.go`, `docker_test.go`, `docker_testing_test.go`, `docker_version_test.go`
- Modify: `backend/monitoring/internal/app/system.go` or wherever `GetHostInfo` is consumed (`dockerapi.HostInfo` fields become `system.Info` fields: `OperatingSystem`, `KernelVersion`, `NCPU`, `MemTotal`)

**Interfaces:**
- `Manager` fields change: `engine *engineClient` replaces `client *http.Client`, `buf`, `decoder`, `apiStats`, `apiContainerList`; `GetHostInfo` returns `(system.Info, error)`.
- Everything exported keeps its name and meaning: `NewManager(ctx, onPodmanDetected) *Manager` (nil when `DOCKER_HOST` is empty), `GetStats`, `GetContainerIdentities`, `IsPodman`, `SetCollectorKey`, `ReseedFromCollector`.

- [ ] **Step 1: Rewire construction**

In `NewManager` keep the `DOCKER_HOST`, `getDockerHost`, `DOCKER_TIMEOUT` and `EXCLUDE_CONTAINERS` handling, then:

```go
	engine, err := newEngineClient(dockerHost, timeout)
	if err != nil {
		slog.Error("docker client unavailable", "host", dockerHost, "err", err)
		return nil
	}
	manager := &Manager{
		onPodmanDetected:  onPodmanDetected,
		engine:            engine,
		containerStatsMap: make(map[string]*container.Stats),
		sem:               make(chan struct{}, 5),
		excludeContainers: excludeContainers,
		lastCpuContainer:    make(map[uint16]map[string]uint64),
		lastCpuSystem:       make(map[uint16]map[string]uint64),
		networkSentTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		networkRecvTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		lastNetworkReadTime: make(map[uint16]map[string]time.Time),
	}
```

Replace the two `os.Exit(1)` calls on bad `DOCKER_HOST`/`DOCKER_TIMEOUT` with `slog.Error` plus `return nil`; a daemon must not exit because Docker is misconfigured.

- [ ] **Step 2: Listing and identities**

`GetStats`: replace the `/containers/json` request and decode with `summaries, err := dm.engine.listContainers(ctx, true)`. Iterate `for i := range summaries { ctr := &summaries[i] ... }`; fields map as `ctr.ID`, `ctr.Names`, `ctr.Status`, `ctr.State`, `ctr.Image`, `ctr.Ports`, `ctr.Health`. Compute the short id locally (`idShort := shortContainerID(ctr.ID)`) instead of the `IdShort` DTO field, and pass `idShort` to `updateContainerStats`. Podman detection from the `Server` header goes away; `ensureDockerVersionChecked` already runs `checkDockerVersion`, which detects Podman from the version result (Step 4). `GetContainerIdentities`: `dm.engine.listContainers(ctx, false)` and the same field renames.

- [ ] **Step 3: Stats update**

`updateContainerStats(ctx, ctr *container.Summary, idShort string, cacheTimeMs uint16)`:

```go
	name := strings.TrimPrefix(ctr.Names[0], "/")
	stats, err := dm.engine.containerStats(ctx, idShort)
	if err != nil {
		return err
	}
	statusText, health := parseDockerStatus(ctr.Status)
	if ctr.Health != nil && ctr.Health.Status != "" {
		if h, ok := parseDockerHealthStatus(string(ctr.Health.Status)); ok {
			health = h
		}
	} else if dm.usingPodman {
		if podmanHealth, healthErr := dm.getPodmanContainerHealth(ctx, idShort); healthErr == nil {
			health = podmanHealth
		}
	}
	// ... map bookkeeping unchanged, except the container.Stats map value is
	// named entry here because stats now holds the moby StatsResponse ...
	cpuPct := cpuPercentLinux(stats, prevCpuContainer, prevCpuSystem)
	usedMemory, err := memoryUsage(stats)
	// ... unchanged: validate, network deltas, read time ...
	dm.setCpuCurrentValues(cacheTimeMs, idShort, stats.CPUStats.CPUUsage.TotalUsage, stats.CPUStats.SystemUsage)
	updateContainerStatsValues(entry, cpuPct, usedMemory, sentDelta, recvDelta, stats.Read)
```

`ctr.Health` is a pointer in the moby `Summary`; check with the `rg` from Task 2 and drop the nil check if it is a value. `calculateNetworkStats` takes `stats.Networks` (`map[string]container.NetworkStats`, fields `RxBytes`, `TxBytes`) and the short id. `convertContainerPortsToString` takes `[]container.PortSummary` (fields `IP`, `PublicPort`). `getPodmanContainerHealth` becomes:

```go
func (dm *Manager) getPodmanContainerHealth(ctx context.Context, containerID string) (container.DockerHealth, error) {
	status, err := dm.engine.inspectHealth(ctx, containerID)
	if err != nil {
		return container.DockerHealthNone, err
	}
	if health, ok := parseDockerHealthStatus(status); ok {
		return health, nil
	}
	return container.DockerHealthNone, nil
}
```

Note the two `container` packages: the daemon's own `internal/domain/container` and moby's `api/types/container`. Import moby's as `mobycontainer` throughout `docker.go` to keep the existing `container.Stats` references readable.

- [ ] **Step 4: Version, Podman and host info**

```go
func (dm *Manager) checkDockerVersion(ctx context.Context) (bool, error) {
	version, err := dm.engine.serverVersion(ctx)
	if err != nil {
		return false, err
	}
	dm.applyDockerVersionInfo(version)
	return dm.goodDockerVersion, nil
}

func (dm *Manager) applyDockerVersionInfo(version client.ServerVersionResult) {
	if detectPodmanEngine(version) {
		dm.setIsPodman()
	}
	dm.goodDockerVersion = dm.usingPodman || dockerMajorVersion(version.Version) >= 25
	dm.dockerVersionChecked = true
}

func detectPodmanEngine(version client.ServerVersionResult) bool {
	if strings.Contains(strings.ToLower(version.Platform.Name), "podman") {
		return true
	}
	for _, component := range version.Components {
		if strings.Contains(strings.ToLower(component.Name), "podman") {
			return true
		}
	}
	return false
}
```

Delete `dockerVersionResponse`, `detectPodmanFromHeader`, `detectPodmanFromVersion`; keep `dockerMajorVersion`. `GetHostInfo` returns `dm.engine.hostInfo(ctx)`; update its consumer in `internal/app/system.go` to read `info.OperatingSystem`, `info.KernelVersion`, `info.NCPU`, `info.MemTotal` from `system.Info`.

- [ ] **Step 5: Tests**

- Test servers: every `httptest` handler that matched exact paths (`/containers/json`, `/version`, `/containers/<id>/json`) matches by `strings.HasSuffix` now, because the client prefixes `/v1.xx`. Managers under test are built with `newEngineClient(server.URL, time.Second)` assigned to `dm.engine` (replace `newDockerManagerForVersionTest` accordingly).
- Podman tests that set a `Server: Libpod` header switch to a version payload with `"Platform": {"Name": "Podman Engine"}` or a `Components` entry named `Podman Engine`.
- Tests that construct `dockerapi.Stats` or `dockerapi.Info` literals construct `mobycontainer.StatsResponse` and `mobycontainer.Summary` with the same values; `TestContainerStatsEndToEndWithRealData` decodes `testdata/container.json` into `StatsResponse` and asserts the same CPU, memory and bandwidth numbers it asserts today.
- `TestFormulasMatchLegacyDTOs` from Task 1 stays until Task 4.

```bash
make test-go GO_TEST_PKGS='./monitoring/internal/integration/docker/... ./monitoring/internal/app/...'
```

Expected: PASS.

- [ ] **Step 6: Stage**

```bash
git add backend/monitoring/internal/integration/docker backend/monitoring/internal/app
```

---

### Task 4: Delete the DTOs and finish

**Files:**
- Delete: `backend/monitoring/internal/integration/docker/dockerapi/`
- Modify: `backend/monitoring/internal/integration/docker/stats_test.go` (parity test becomes fixture-value assertions), `backend/go.mod` (via `make golint-only` tidy), `docs/monitoring.md`, `docs/TODO/linuxio-monitoring.md`

- [ ] **Step 1: Freeze the parity numbers, then delete the legacy package**

Run once and record the exact values the legacy formulas produced for the fixtures:

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/integration/docker/... GO_TEST_FLAGS='-run TestFormulasMatchLegacyDTOs -v'
```

Rewrite `TestFormulasMatchLegacyDTOs` as `TestFormulasMatchRecordedFixtureValues` that asserts those literal numbers (CPU percent at half-previous counters, CPU percent with zero previous, memory bytes) for `container.json` and `container2.json`, without importing `dockerapi`. Then:

```bash
git rm -rq backend/monitoring/internal/integration/docker/dockerapi
rg -n "dockerapi" backend/monitoring
```

Expected: no matches.

- [ ] **Step 2: Lint, tidy, test**

```bash
make golint-only
make test-go GO_TEST_PKGS='./monitoring/...'
```

Expected: clean and PASS; `backend/go.mod` keeps `github.com/moby/moby/client` and `github.com/moby/moby/api` as direct requirements (they already are, for the bridge).

- [ ] **Step 3: Docs**

`docs/monitoring.md`: the Docker integration uses the moby client; Podman is detected from the server version. `docs/TODO/linuxio-monitoring.md`: status line "Implemented" and move the plan's completion note into `docs/TODO/completed.md` following that file's format, then remove the plan from the TODO index.

- [ ] **Step 4: Full verification**

```bash
make check-backend-quiet
make test-quiet
```

Expected: PASS.

- [ ] **Step 5: Runtime**

`sudo make localinstall`; on this host with Docker running:

```bash
curl -s --unix-socket /run/linuxio/monitoring/api.sock http://localhost/api/v1/containers | jq '.items | length'
curl -s --unix-socket /run/linuxio/monitoring/api.sock http://localhost/api/v1/live | jq '.containers.items[0]'
```

Expected: the same container count as `docker ps -a`, and a first container item with plausible CPU, memory and network numbers; the Docker page in the browser shows per-container metrics as before.

- [ ] **Step 6: Stage**

```bash
git add backend docs
```

Suggested commit message for the user:

```
refactor(monitoring): use the moby client for container statistics

Replace the raw Docker Engine HTTP client and hand-written DTOs with the
typed client the bridge already depends on. Formulas, delta tracking,
Podman detection and the pre-25 retry path are unchanged and verified
against the recorded fixture values.
```

---

## Self-Review Notes

- Spec coverage: moby client replaces the raw client (Tasks 2, 3), trackers, Podman detection, concurrency and exclusions kept (Task 3), fixture parity (Tasks 1, 4), nothing else depends on it (all tasks confined to the docker package plus one host-info consumer).
- Type consistency: `engineClient` methods (Task 2) are the only transport used in Task 3; `cpuPercentLinux` and `memoryUsage` (Task 1) take `*mobycontainer.StatsResponse`, which `containerStats` returns.
