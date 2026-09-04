# linuxio-monitoring Plan 1: Daemon Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `linuxio-monitoring`, the go-monitoring agent imported as a first-party LinuxIO binary with fixed unix sockets, strict YAML config, optional read-only TCP listeners with plugin allowlists, a byte-precise `/api/v1/live` route, and the existing bridge monitoring routes repointed to it.

**Architecture:** The go-monitoring tree is copied under `backend/monitoring/internal/` with import paths rewritten and its CLI, logging, health-file, benchmark, pprof and systemd-plugin code dropped. A new `internal/daemon` package owns the process: it loads YAML config, opens two fixed unix sockets (`api.sock` 0666 read-only, `control.sock` 0600 root-only with peer-uid check) plus configured listeners, and hosts the command executor. Live requests keep per-request collection but reuse a sample under one second old and reseed stale baselines from the last collector tick. The bridge's existing monitoring package talks to the two sockets; gauges stay bridge-side until Plan 2.

**Tech Stack:** Go 1.27, `github.com/mattn/go-sqlite3` (cgo), `github.com/goccy/go-yaml`, `github.com/shirou/gopsutil/v4`, `github.com/ebitengine/purego` (NVML, `amd64 && glibc` tags), systemd units, GitHub Actions release workflow, React/TanStack frontend for the settings section.

**Spec:** `docs/TODO/linuxio-monitoring.md`

## Global Constraints

- Never create, amend or push a Git commit. Stage files by explicit path with `git add <path>`; the user commits.
- Run Go, frontend and generation work only through Make targets (`make test-go GO_TEST_PKGS=... GO_TEST_FLAGS=...`, `make golint-only`, `make generate`, `make check-backend-quiet`, `make check-frontend-quiet`, `make test-quiet`). Never call `go test`, `gofmt`, `golangci-lint`, `npm`, `vitest` directly. Quiet logs live in `.cache/test-logs/`.
- Do not hand-edit `frontend/src/api/generated/*` or `frontend/src/routeTree.gen.ts`; run `make generate` after apischema changes.
- Module path is `github.com/mordilloSan/LinuxIO/backend`. New code lives under `backend/monitoring/`. Daemon internals are under `backend/monitoring/internal/`; only `backend/monitoring/api` is importable by the bridge.
- Binary `linuxio-monitoring`; unit `linuxio-monitoring.service`; sockets `/run/linuxio/monitoring/api.sock` (mode 0666) and `/run/linuxio/monitoring/control.sock` (mode 0600, peer uid 0 only); database `/var/lib/linuxio/monitoring/metrics.db`; config `/etc/linuxio/monitoring/config.yaml`.
- Commands are served only on `control.sock`. Configured listeners serve the read-only metrics API only. `allow_remote_commands` does not exist.
- Live requests: reuse a sample for the same key when it is under 1 second old or still in flight; reseed a live key's baseline from the last collector tick when the baseline is older than that tick. REST current endpoints otherwise keep per-request semantics.
- No background sampling tick. No migration from an installed go-monitoring.
- Dropped dependencies: `spf13/pflag`, `x/term`, `coreos/go-systemd`, `modernc.org/sqlite`, `google/uuid`. Added: `github.com/ebitengine/purego`.
- In Go: caller `context.Context` first on blocking work, `%w` wrapping, no panics for runtime failures, every goroutine has an owner and exit path.
- Prefer the smallest change that fits the surrounding package. Do not refactor copied code beyond what a task names.

---

## File Structure

**Created**

| Path | Responsibility |
|---|---|
| `backend/monitoring/main.go` | Process entry; the only `os.Exit`. |
| `backend/monitoring/internal/cli/main.go` | `run [--config] [--verbose]`, `--version`, `--help`. |
| `backend/monitoring/internal/daemon/daemon.go` | Loads config, builds listeners (two fixed sockets plus configured), starts the app. |
| `backend/monitoring/internal/daemon/command.go` | Command executor moved from go-monitoring `cmd/command_api.go`. |
| `backend/monitoring/internal/config/config.go` | Strict YAML config, validation, flat JSON view for the command API. |
| `backend/monitoring/internal/app/live_reuse.go` | One-second reuse of live samples per key. |
| `backend/monitoring/internal/app/live_api.go` | Builds `api.Live` from a live sample. |
| `backend/monitoring/internal/app/disk_devices.go` | Per-block-device I/O rates and the physical-disk filter moved from the bridge. |
| `backend/monitoring/internal/app/peer_gate.go` | Root-only wrapper for the control socket. |
| `backend/monitoring/api/live.go` | `Live` wire type and socket path constants shared with the bridge. |
| `backend/common/peercred/peercred.go` | `SO_PEERCRED` helpers usable by any daemon. |
| `packaging/systemd/linuxio-monitoring.service` | Unit. |
| `packaging/etc/linuxio/monitoring/config.yaml` | Default config shipped by the installers. |
| `docs/monitoring.md` | Service guide in the shape of `docs/indexer.md`. |

**Copied from go-monitoring** (import paths rewritten): `internal/app`, `internal/store`, `internal/api/http`, `internal/api/model`, `internal/domain/{container,network,process,smart,system}`, `internal/integration/docker`, `internal/deltatracker`, `internal/utils`, `internal/defaults`, plus their tests and `testdata`.

**Modified**

| Path | Change |
|---|---|
| `Makefile` | `build-monitoring`, `_build-binaries`, `clean`, help. |
| `.github/workflows/release.yml` | Build, verify, tar, checksum, artifact. |
| `packaging/scripts/localinstall.sh`, `install-linuxio-binaries.sh`, `uninstall.sh` | Binary and unit lists, config dir. |
| `packaging/man/linuxio.8`, `backend/cli/main.go` | `logs monitoring`, version listing. |
| `backend/bridge/handlers/monitoring/*` | Two fixed sockets, live-based container metrics, new config fields. |
| `backend/bridge/handlers/system/capabilities.go` | `/healthz` detection, install spec removed. |
| `backend/bridge/handlers/packages/install_capability.go` | Optional-component branch removed. |
| `backend/bridge/apischema/models.go` | Monitoring config, listener, status types. |
| `frontend/src/api/capabilities.ts`, `capabilities.test.ts` | Built-in capability entry. |
| `frontend/src/routes/_authenticated/-components/navbar/MonitoringSettingsSection.tsx` | New fields, listener plugins. |
| `docs/THIRD_PARTY_NOTICES.md`, `docs/process-systemd-architecture.md`, `docs/capabilities.md`, `README.md` | Attribution and service docs. |

**Deleted**: `backend/bridge/handlers/packages/install_monitoring.go` and `_test.go`.

---

### Task 1: Import the go-monitoring tree

**Files:**
- Create: `backend/monitoring/internal/**` (copied)
- Modify: `backend/go.mod`, `backend/go.sum`

**Interfaces:**
- Produces: packages `github.com/mordilloSan/LinuxIO/backend/monitoring/internal/{app,store,config,api/http,api/model,domain/...,integration/docker,deltatracker,utils,defaults}` that compile, with `version.Version` coming from `backend/common/version` and logging set up by callers.

- [ ] **Step 1: Clone the source at the tag LinuxIO currently runs**

```bash
rm -rf /tmp/go-monitoring-import
git clone --quiet --branch v1.7.0 --depth 1 https://github.com/mordilloSan/go-monitoring.git /tmp/go-monitoring-import
```

- [ ] **Step 2: Copy the kept packages and drop the rest**

```bash
cd /home/miguelmariz/LinuxIO
SRC=/tmp/go-monitoring-import
mkdir -p backend/monitoring/internal/daemon backend/monitoring/api backend/monitoring/internal/cli
for pkg in app store config api domain integration deltatracker utils defaults; do
  cp -R "$SRC/internal/$pkg" backend/monitoring/internal/
done
rm -rf backend/monitoring/internal/domain/systemd
rm -f backend/monitoring/internal/app/systemd.go backend/monitoring/internal/app/systemd_test.go
rm -f backend/monitoring/internal/api/http/benchmark.go backend/monitoring/internal/api/http/debug.go
cp "$SRC/cmd/command_api.go" backend/monitoring/internal/daemon/command.go
cp "$SRC/cmd/command_api_test.go" backend/monitoring/internal/daemon/command_test.go
```

- [ ] **Step 3: Rewrite import paths (version first, then the generic prefix)**

```bash
cd /home/miguelmariz/LinuxIO
find backend/monitoring -name '*.go' -print0 | xargs -0 sed -i \
  -e 's#github.com/mordilloSan/go-monitoring/internal/version#github.com/mordilloSan/LinuxIO/backend/common/version#g' \
  -e 's#github.com/mordilloSan/go-monitoring/internal/#github.com/mordilloSan/LinuxIO/backend/monitoring/internal/#g'
sed -i 's/^package cmd$/package daemon/' backend/monitoring/internal/daemon/command.go backend/monitoring/internal/daemon/command_test.go
rg -n "go-monitoring/internal|internal/logging|internal/health|internal/leakcheck|buildinfo\." backend/monitoring --type go
```

Expected: the last `rg` lists only the `logging` and `health` imports in `internal/app/agent.go`, `internal/app/runtime.go`, `internal/api/http/server.go` and the `benchmark`/`debug` references removed in Task 4. Everything else must be silent.

- [ ] **Step 4: Remove the logging dependency from `internal/app/agent.go`**

Delete the `configureLogging` method and its call in `New`, and the `logging` import. The `LOG_LEVEL` environment variable is replaced by the CLI `--verbose` flag in Task 9. Keep the `slog.Info("Starting go-monitoring", ...)` line but change the message to `"starting linuxio-monitoring"`.

- [ ] **Step 5: Add dependencies and compile the copied tree**

```bash
cd /home/miguelmariz/LinuxIO/backend && go get github.com/ebitengine/purego@v0.10.2 >/dev/null 2>&1 || true
```

Then, from the repo root:

```bash
make test-go GO_TEST_PKGS=./monitoring/... GO_TEST_FLAGS='-run XXX_NONE'
```

Expected: compile errors only in the three files that still import `health` (fixed in Task 4) and in `command.go` (fixed in Task 9). Record the list; no other packages may fail. If `go get` above is needed, `make golint-only` later runs `go mod tidy` and updates `go.sum`; the `purego` requirement must appear in `backend/go.mod` as a direct dependency and `modernc.org/sqlite`, `spf13/pflag`, `golang.org/x/term`, `coreos/go-systemd`, `google/uuid` must not appear as direct dependencies after Task 3 and Task 9.

- [ ] **Step 6: Stage**

```bash
git add backend/monitoring backend/go.mod backend/go.sum
```

---

### Task 2: Remove the systemd plugin

**Files:**
- Modify: `backend/monitoring/internal/store/plugins.go`, `backend/monitoring/internal/store/history_aggregate.go`, `backend/monitoring/internal/domain/system/system.go`, `backend/monitoring/internal/app/agent.go`, `backend/monitoring/internal/app/runtime.go`, `backend/monitoring/internal/app/live_current.go`, `backend/monitoring/internal/api/http/*_test.go`, `backend/monitoring/internal/store/*_test.go`

**Interfaces:**
- Produces: `store.PluginNames()` without `"systemd"`; `system.CombinedData` without `SystemdServices`; `App` without `systemdManager`.

- [ ] **Step 1: List every reference**

```bash
rg -n -i "systemd" /home/miguelmariz/LinuxIO/backend/monitoring --type go
```

- [ ] **Step 2: Remove the plugin from the store**

In `store/plugins.go` delete the `PluginSystemd = "systemd"` constant, its entry in `pluginNames`, and the `PluginSystemd: nonNilSlice(data.SystemdServices)` line in `SnapshotPluginPayloads`. In `store/history_aggregate.go` delete the `case PluginSystemd` branch if present. In `domain/system/system.go` delete the `SystemdServices []*systemd.Service` field and the `systemd` import.

- [ ] **Step 3: Remove the manager from the app**

In `app/agent.go` delete the `systemdManager *systemdManager` field, the `app.systemdManager, err = newSystemdManager()` block in `New`, the `attachSystemdStats` method and its call in `attachDefaultIntervalStats`, and the `data.Info.Services` assignment. In `app/runtime.go` delete the systemd branches of `startManagers` and `stopManagers`. In `app/live_current.go` delete the `case store.PluginSystemd` branch of `collectCurrentPlugin` and remove `store.PluginSystemd` from the slice in `collectCurrentStandaloneBatch`.

- [ ] **Step 4: Fix tests that name the plugin**

```bash
rg -n -i "systemd" /home/miguelmariz/LinuxIO/backend/monitoring --type go -g '*_test.go'
```

Delete assertions and fixtures that expect a `systemd` plugin or route. Where a test iterates all plugins, the removal needs no edit.

- [ ] **Step 5: Compile and run the store and app tests**

```bash
make test-go GO_TEST_PKGS='./monitoring/internal/store/... ./monitoring/internal/domain/...'
```

Expected: PASS (store tests may still fail on modernc-specific assertions; those are fixed in Task 3, note them).

- [ ] **Step 6: Stage**

```bash
git add backend/monitoring
```

---

### Task 3: Move the store to the repo's cgo sqlite driver

**Files:**
- Modify: `backend/monitoring/internal/store/store.go`, `backend/monitoring/internal/store/store_test.go`

**Interfaces:**
- Produces: `store.OpenStore(dataDir string, options ...Options) (*Store, error)` unchanged in signature, backed by `github.com/mattn/go-sqlite3`.

- [ ] **Step 1: Write the failing test for corruption detection**

Append to `store_test.go`:

```go
func TestRecoverableStoreOpenErrorMatchesMattnCodes(t *testing.T) {
	if !recoverableStoreOpenError(sqlite3.Error{Code: sqlite3.ErrCorrupt}) {
		t.Fatal("ErrCorrupt should be recoverable")
	}
	if !recoverableStoreOpenError(sqlite3.Error{Code: sqlite3.ErrNotADB}) {
		t.Fatal("ErrNotADB should be recoverable")
	}
	if recoverableStoreOpenError(sqlite3.Error{Code: sqlite3.ErrBusy}) {
		t.Fatal("ErrBusy must not be recoverable")
	}
	if recoverableStoreOpenError(errors.New("plain")) {
		t.Fatal("non-sqlite errors must not be recoverable")
	}
}
```

Add `sqlite3 "github.com/mattn/go-sqlite3"` and `"errors"` to the test imports.

- [ ] **Step 2: Run it to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/store/... GO_TEST_FLAGS='-run TestRecoverableStoreOpenErrorMatchesMattnCodes'
```

Expected: FAIL to compile (`sqlite3.Error` undefined with the modernc import).

- [ ] **Step 3: Switch the driver**

In `store.go` replace the imports

```go
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
```

with

```go
	sqlite3 "github.com/mattn/go-sqlite3"
```

Replace both `sql.Open("sqlite", dbPath)` calls with `sql.Open("sqlite3", dbPath)`. Replace `recoverableStoreOpenError`:

```go
func recoverableStoreOpenError(err error) bool {
	serr, ok := errors.AsType[sqlite3.Error](err)
	if !ok {
		return false
	}
	return serr.Code == sqlite3.ErrCorrupt || serr.Code == sqlite3.ErrNotADB
}
```

- [ ] **Step 4: Run the whole store package**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/store/...
```

Expected: PASS. If a test asserts a modernc-specific error string, change the assertion to `errors.AsType[sqlite3.Error]` plus the code.

- [ ] **Step 5: Stage**

```bash
git add backend/monitoring/internal/store
```

---

### Task 4: Replace the health file with in-process collector freshness

**Files:**
- Modify: `backend/monitoring/internal/app/runtime.go`, `backend/monitoring/internal/app/agent.go`, `backend/monitoring/internal/api/http/server.go`, `backend/monitoring/internal/api/http/server_test.go`

**Interfaces:**
- Produces: `func (a *App) LastCollected() (time.Time, bool)`; `httpapi.Options.LastCollected func() (time.Time, bool)`; `GET /healthz` returns 200 while the last collector tick is under twice the collector interval old, 503 otherwise or before the first tick.

- [ ] **Step 1: Write the failing handler test**

Append to `server_test.go`:

```go
func TestHealthzUsesLastCollectedAge(t *testing.T) {
	last := time.Now().Add(-30 * time.Second)
	srv := NewServer(Options{
		LastCollected: func() (time.Time, bool) { return last, true },
	})
	handler := srv.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	stale := NewServer(Options{
		LastCollected: func() (time.Time, bool) { return time.Now().Add(-5 * time.Minute), true },
	})
	rec = httptest.NewRecorder()
	stale.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("stale status = %d, want 503", rec.Code)
	}

	never := NewServer(Options{LastCollected: func() (time.Time, bool) { return time.Time{}, false }})
	rec = httptest.NewRecorder()
	never.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no-sample status = %d, want 503", rec.Code)
	}
}
```

The third `HandlerFor` argument (plugin allowlist) is added in Task 5; until then pass only two arguments and add the third when Task 5 lands. Existing `HandlerFor` callers in tests get the extra `nil` in Task 5.

- [ ] **Step 2: Run it to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/api/http/... GO_TEST_FLAGS='-run TestHealthzUsesLastCollectedAge'
```

Expected: FAIL (`Options` has no field `LastCollected`).

- [ ] **Step 3: Implement in the HTTP server**

In `server.go` remove the `health` import, add `lastCollected func() (time.Time, bool)` to `Server` and `LastCollected func() (time.Time, bool)` to `Options`, copy it in `NewServer`, and register `mux.HandleFunc("/healthz", s.handleHealth(collectorInterval))`. Replace `handleHealth`:

```go
func (s *Server) handleHealth(collectorInterval func() time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		if s.lastCollected == nil {
			writeError(w, http.StatusServiceUnavailable, errors.New("collector state unavailable"))
			return
		}
		last, ok := s.lastCollected()
		if !ok {
			writeError(w, http.StatusServiceUnavailable, errors.New("no collector sample yet"))
			return
		}
		interval := time.Minute
		if collectorInterval != nil {
			if configured := collectorInterval(); configured > 0 {
				interval = configured
			}
		}
		age := time.Since(last)
		healthy := age <= 2*interval
		code := http.StatusOK
		if !healthy {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{
			"healthy":      healthy,
			"last_updated": last.UTC(),
			"age_seconds":  age.Seconds(),
		})
	}
}
```

- [ ] **Step 4: Track the last tick in the app**

In `agent.go` add `lastCollectedMs atomic.Int64` to `App` (import `sync/atomic`). In `runtime.go`:

- in `collectAndPersist`, replace `if err := health.Update(); err != nil { return err }` with `a.lastCollectedMs.Store(capturedAt)`;
- in `StartContext`, delete the `health.CleanUp()` block;
- in `apiServer`, add `LastCollected: a.LastCollected,` to the options;
- remove the `health` import and add:

```go
// LastCollected reports when the collector last persisted a sample.
func (a *App) LastCollected() (time.Time, bool) {
	ms := a.lastCollectedMs.Load()
	if ms == 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}
```

- [ ] **Step 5: Run the tests**

```bash
make test-go GO_TEST_PKGS='./monitoring/internal/api/http/... ./monitoring/internal/app/...'
```

Expected: PASS for `api/http`; `app` compiles (its remaining failures, if any, must be listed and belong to Task 7 or Task 8).

- [ ] **Step 6: Stage**

```bash
git add backend/monitoring/internal/app backend/monitoring/internal/api
```

---

### Task 5: Listener modes, root-only control socket, plugin allowlists

**Files:**
- Create: `backend/common/peercred/peercred.go`, `backend/common/peercred/peercred_test.go`, `backend/monitoring/internal/app/peer_gate.go`, `backend/monitoring/internal/app/peer_gate_test.go`
- Modify: `backend/monitoring/internal/app/runtime.go` (`ListenerOptions`, `startHTTPServers`), `backend/monitoring/internal/app/listen.go` (`openListener`), `backend/monitoring/internal/api/http/server.go` (`HandlerFor`), `backend/monitoring/internal/api/http/plugin.go` (`NewRegistry`), `backend/monitoring/internal/api/http/plugin_test.go` or `server_test.go`

**Interfaces:**
- Produces:
  - `peercred.ConnContext(ctx context.Context, c net.Conn) context.Context` and `peercred.UID(ctx context.Context) (uint32, bool)`.
  - `app.ListenerOptions{Name, Address string; APIs []string; Mode os.FileMode; RootOnly bool; Plugins []string; BestEffort bool}`.
  - `httpapi.NewRegistry(current CurrentReader, metrics MetricsReader, refresher SmartRefresher, allowed []string) *Registry` (nil `allowed` means every plugin).
  - `(*Server).HandlerFor(collectorInterval func() time.Duration, apis []string, plugins []string) http.Handler`; summary route mounted only when `plugins == nil`.

- [ ] **Step 1: Write the peercred package and test**

`backend/common/peercred/peercred.go`:

```go
// Package peercred reads SO_PEERCRED for unix-socket HTTP servers so handlers
// can gate on the connecting process's uid.
package peercred

import (
	"context"
	"net"
	"net/http"
	"syscall"
)

type contextKey struct{}

type Cred struct {
	UID uint32
	GID uint32
}

// ConnContext is an http.Server.ConnContext hook. It attaches the peer
// credentials of a unix connection; other connections pass through.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return ctx
	}
	cred, err := read(uc)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, cred)
}

// UID returns the peer uid attached by ConnContext.
func UID(ctx context.Context) (uint32, bool) {
	cred, ok := ctx.Value(contextKey{}).(Cred)
	if !ok {
		return 0, false
	}
	return cred.UID, true
}

// RequestUID is UID for an HTTP request.
func RequestUID(r *http.Request) (uint32, bool) {
	return UID(r.Context())
}

func read(conn *net.UnixConn) (Cred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return Cred{}, err
	}
	var ucred *syscall.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, sockErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return Cred{}, err
	}
	if sockErr != nil {
		return Cred{}, sockErr
	}
	return Cred{UID: ucred.Uid, GID: ucred.Gid}, nil
}
```

`backend/common/peercred/peercred_test.go`:

```go
package peercred

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestConnContextAttachesOwnUID(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan uint32, 1)
	srv := &http.Server{
		ConnContext: ConnContext,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := RequestUID(r)
			if !ok {
				t.Error("no peer uid on request")
			}
			got <- uid
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	resp, err := client.Get("http://unix/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if uid := <-got; uid != uint32(os.Getuid()) {
		t.Fatalf("uid = %d, want %d", uid, os.Getuid())
	}
}

func TestUIDAbsentOnPlainContext(t *testing.T) {
	if _, ok := UID(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("expected no uid")
	}
}
```

- [ ] **Step 2: Run the peercred test**

```bash
make test-go GO_TEST_PKGS=./common/peercred/...
```

Expected: PASS.

- [ ] **Step 3: Write the failing allowlist tests**

Append to `backend/monitoring/internal/api/http/server_test.go` (reuse the package's existing fake `CurrentReader`/`MetricsReader` test doubles; the file already constructs a `Server` for route tests):

```go
func TestPluginAllowlistFiltersRoutes(t *testing.T) {
	srv := newTestServer(t) // existing helper in this file; adapt the name if it differs
	handler := srv.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, []string{"cpu"})

	for path, want := range map[string]int{
		"/api/v1/cpu":            http.StatusOK,
		"/api/v1/mem":            http.StatusNotFound,
		"/api/v1/system/summary": http.StatusNotFound,
		"/api/v1/plugins":        http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Fatalf("%s: status = %d, want %d", path, rec.Code, want)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil))
	if body := rec.Body.String(); strings.Contains(body, `"name":"mem"`) {
		t.Fatalf("plugins listing leaks mem: %s", body)
	}
}
```

- [ ] **Step 4: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/api/http/... GO_TEST_FLAGS='-run TestPluginAllowlistFiltersRoutes'
```

Expected: FAIL (wrong argument count).

- [ ] **Step 5: Implement the registry filter and `HandlerFor`**

In `plugin.go` change `NewRegistry`:

```go
func NewRegistry(current CurrentReader, metrics MetricsReader, refresher SmartRefresher, allowed []string) *Registry {
	allow := map[string]bool{}
	for _, name := range allowed {
		allow[strings.ToLower(strings.TrimSpace(name))] = true
	}
	registry := &Registry{metrics: metrics, current: current, byName: map[string]Plugin{}}
	for _, name := range store.PluginNames() {
		if allowed != nil && !allow[name] {
			continue
		}
		// ... existing body unchanged from here ...
```

In `server.go`:

```go
func (s *Server) HandlerFor(collectorInterval func() time.Duration, apis []string, plugins []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth(collectorInterval))
	if hasAPI(apis, "metrics") {
		mux.HandleFunc("/api/v1/meta", s.handleMeta(collectorInterval))
		if plugins == nil {
			mux.HandleFunc("/api/v1/system/summary", s.handleSystemSummary)
		}
		NewRegistry(s.current, s.metrics, s.smartRefresher, plugins).Mount(mux, "/api/v1/")
	}
	if hasAPI(apis, "commands") {
		mux.HandleFunc("/api/v1/command", s.handleCommand)
	}
	if s.requestLogging {
		return logRequests(mux)
	}
	return mux
}
```

Delete `mountDebug` references and the `debug` API kind wherever `server.go` still names them. Update every existing `HandlerFor` and `NewRegistry` call in tests to pass `nil` for the new argument.

- [ ] **Step 6: Extend listener options and socket modes**

In `runtime.go`:

```go
type ListenerOptions struct {
	Name       string
	Address    string
	APIs       []string
	Mode       os.FileMode // unix sockets only; 0 means 0o660
	RootOnly   bool        // reject peers whose uid is not 0
	Plugins    []string    // nil means every metrics plugin
	BestEffort bool
}
```

In `startHTTPServers` replace `listener, err := openListener(listenerOpts.Address)` with `listener, err := openListener(listenerOpts.Address, listenerOpts.Mode)`, build the handler as

```go
			handler := a.apiServer(opts.CommandExecutor).HandlerFor(a.CollectorInterval, listenerOpts.APIs, listenerOpts.Plugins)
			if listenerOpts.RootOnly {
				handler = requireRootPeer(handler)
			}
```

and set `ConnContext: peercred.ConnContext,` on the `http.Server` when `listenerOpts.RootOnly` is true. In `listen.go`:

```go
func openListener(addr string, mode os.FileMode) (net.Listener, error) {
	network, address := SplitListenAddress(addr)
	if network == "unix" {
		if err := prepareUnixSocket(address); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	if network == "unix" {
		if mode == 0 {
			mode = 0o660
		}
		if err := os.Chmod(address, mode); err != nil {
			_ = listener.Close()
			return nil, err
		}
	}
	return listener, nil
}
```

Update the existing `openListener` callers and tests for the new argument.

- [ ] **Step 7: Add the root gate and its test**

`backend/monitoring/internal/app/peer_gate.go`:

```go
package app

import (
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
)

// requireRootPeer serves the control socket: only a root peer may call it.
func requireRootPeer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := peercred.RequestUID(r)
		if !ok || uid != 0 {
			http.Error(w, "this endpoint requires a root peer", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

`peer_gate_test.go`:

```go
package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
)

func TestRequireRootPeerRejectsMissingAndNonRoot(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	gate := requireRootPeer(next)

	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no cred: %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(peercred.WithCredForTest(context.Background(), 1000))
	rec = httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("uid 1000: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(peercred.WithCredForTest(context.Background(), 0))
	rec = httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("uid 0: %d", rec.Code)
	}
}
```

Add to `peercred.go`:

```go
// WithCredForTest attaches a uid without a socket. Tests only.
func WithCredForTest(ctx context.Context, uid uint32) context.Context {
	return context.WithValue(ctx, contextKey{}, Cred{UID: uid})
}
```

- [ ] **Step 8: Run**

```bash
make test-go GO_TEST_PKGS='./common/peercred/... ./monitoring/internal/api/http/... ./monitoring/internal/app/...' GO_TEST_FLAGS='-run "TestPluginAllowlist|TestRequireRootPeer|TestHealthz|TestConnContext"'
```

Expected: PASS.

- [ ] **Step 9: Stage**

```bash
git add backend/common/peercred backend/monitoring/internal/app backend/monitoring/internal/api
```

---

### Task 6: Strict YAML config

**Files:**
- Rewrite: `backend/monitoring/internal/config/config.go`, `backend/monitoring/internal/config/config_test.go`

**Interfaces:**
- Produces:

```go
package config

const CurrentVersion = 1
const DefaultPath = "/etc/linuxio/monitoring/config.yaml"

type Duration time.Duration // YAML and JSON as Go duration strings

type Config struct {
	Version   int        `yaml:"version"`
	Collector Collector  `yaml:"collector"`
	History   History    `yaml:"history"`
	Listeners []Listener `yaml:"listeners"`
}
type Collector struct {
	Interval             Duration `yaml:"interval"`
	SmartRefreshInterval Duration `yaml:"smart_refresh_interval"`
	DiskUsageCache       Duration `yaml:"disk_usage_cache"`
}
type History struct {
	Retention Duration `yaml:"retention"`
	Plugins   []string `yaml:"plugins"`
}
type Listener struct {
	Name    string   `yaml:"name" json:"name"`
	Address string   `yaml:"address" json:"address"`
	Plugins []string `yaml:"plugins,omitempty" json:"plugins,omitempty"`
}

// View is the flat JSON shape served by config.get and accepted by config.set.
type View struct {
	Version              int        `json:"version"`
	CollectorInterval    string     `json:"collector_interval"`
	SmartRefreshInterval string     `json:"smart_refresh_interval"`
	DiskUsageCache       string     `json:"disk_usage_cache"`
	HistoryRetention     string     `json:"history_retention"`
	History              string     `json:"history"` // comma-separated plugin list
	Listeners            []Listener `json:"listeners"`
}

func Default() Config
func Load(path string) (cfg Config, loaded bool, err error) // strict; absent file returns Default(), false, nil
func Save(path string, cfg Config) error                  // atomic 0644 write
func SaveIfMissing(path string, cfg Config) (created bool, err error)
func Validate(cfg Config) error
func (c Config) View() View
func (c Config) HistoryString() string
```

- [ ] **Step 1: Write the failing tests**

Replace `config_test.go` with:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAbsentFileReturnsDefaults(t *testing.T) {
	cfg, loaded, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil || loaded {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	if cfg.Collector.Interval != Duration(time.Minute) || cfg.History.Retention != Duration(720*time.Hour) {
		t.Fatalf("defaults = %+v", cfg)
	}
	if len(cfg.Listeners) != 0 {
		t.Fatalf("default listeners must be empty, got %v", cfg.Listeners)
	}
}

func TestLoadParsesNestedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
version: 1
collector:
  interval: 30s
  smart_refresh_interval: 2h
  disk_usage_cache: 10m
history:
  retention: 48h
  plugins: [cpu, mem]
listeners:
  - name: homepage
    address: 0.0.0.0:45876
    plugins: [cpu, network]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, loaded, err := Load(path)
	if err != nil || !loaded {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	if cfg.Collector.Interval != Duration(30*time.Second) || cfg.Collector.DiskUsageCache != Duration(10*time.Minute) {
		t.Fatalf("collector = %+v", cfg.Collector)
	}
	if cfg.HistoryString() != "cpu,mem" {
		t.Fatalf("history = %q", cfg.HistoryString())
	}
	if len(cfg.Listeners) != 1 || cfg.Listeners[0].Plugins[1] != "network" {
		t.Fatalf("listeners = %+v", cfg.Listeners)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte("version: 1\nallow_remote_commands: true\n"), 0o644)
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "allow_remote_commands") {
		t.Fatalf("expected unknown key error, got %v", err)
	}
}

func TestValidateRejectsBadListeners(t *testing.T) {
	cfg := Default()
	cfg.Listeners = []Listener{{Name: "a", Address: "127.0.0.1:1", Plugins: []string{"nope"}}}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "unknown plugin") {
		t.Fatalf("plugin validation: %v", err)
	}
	cfg.Listeners = []Listener{{Name: "a", Address: "127.0.0.1:1"}, {Name: "a", Address: "127.0.0.1:2"}}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "duplicate listener name") {
		t.Fatalf("name validation: %v", err)
	}
	cfg.Listeners = []Listener{{Name: "a", Address: "unix:/run/linuxio/monitoring/api.sock"}}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved socket validation: %v", err)
	}
	cfg.Listeners = nil
	cfg.Collector.Interval = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("zero interval must fail")
	}
}

func TestSaveRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg := Default()
	cfg.Collector.Interval = Duration(15 * time.Second)
	cfg.Listeners = []Listener{{Name: "lan", Address: ":9000", Plugins: []string{"cpu"}}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, loaded, err := Load(path)
	if err != nil || !loaded {
		t.Fatalf("reload: loaded=%v err=%v", loaded, err)
	}
	if again.Collector.Interval != cfg.Collector.Interval || again.Listeners[0].Plugins[0] != "cpu" {
		t.Fatalf("round trip mismatch: %+v", again)
	}
	created, err := SaveIfMissing(path, Default())
	if err != nil || created {
		t.Fatalf("SaveIfMissing on existing: created=%v err=%v", created, err)
	}
}

func TestViewFlattens(t *testing.T) {
	view := Default().View()
	if view.CollectorInterval != "1m0s" || view.History != strings.Join(Default().History.Plugins, ",") || view.Listeners == nil {
		t.Fatalf("view = %+v", view)
	}
}
```

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/config/...
```

Expected: FAIL to compile.

- [ ] **Step 3: Rewrite `config.go`**

```go
// Package config loads and validates the linuxio-monitoring YAML config.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/app"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/defaults"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
)

const (
	CurrentVersion = 1
	DefaultPath    = "/etc/linuxio/monitoring/config.yaml"
)

// Duration serialises as a Go duration string in YAML and JSON.
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var raw string
	if err := unmarshal(&raw); err != nil {
		return err
	}
	return d.parse(raw)
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return d.parse(raw)
}

func (d *Duration) parse(raw string) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if parsed < 0 {
		return errors.New("duration must not be negative")
	}
	*d = Duration(parsed)
	return nil
}

type Config struct {
	Version   int        `yaml:"version"`
	Collector Collector  `yaml:"collector"`
	History   History    `yaml:"history"`
	Listeners []Listener `yaml:"listeners"`
}

type Collector struct {
	Interval             Duration `yaml:"interval"`
	SmartRefreshInterval Duration `yaml:"smart_refresh_interval"`
	DiskUsageCache       Duration `yaml:"disk_usage_cache"`
}

type History struct {
	Retention Duration `yaml:"retention"`
	Plugins   []string `yaml:"plugins"`
}

type Listener struct {
	Name    string   `yaml:"name" json:"name"`
	Address string   `yaml:"address" json:"address"`
	Plugins []string `yaml:"plugins,omitempty" json:"plugins,omitempty"`
}

// View is the flat JSON shape served by config.get and accepted by config.set.
type View struct {
	Version              int        `json:"version"`
	CollectorInterval    string     `json:"collector_interval"`
	SmartRefreshInterval string     `json:"smart_refresh_interval"`
	DiskUsageCache       string     `json:"disk_usage_cache"`
	HistoryRetention     string     `json:"history_retention"`
	History              string     `json:"history"`
	Listeners            []Listener `json:"listeners"`
}

const defaultDiskUsageCache = 0 // re-read usage on every collection; set to keep sleeping disks asleep

func Default() Config {
	return Config{
		Version: CurrentVersion,
		Collector: Collector{
			Interval:             Duration(defaults.CollectorInterval),
			SmartRefreshInterval: Duration(defaults.SmartRefreshInterval),
			DiskUsageCache:       Duration(defaultDiskUsageCache),
		},
		History: History{
			Retention: Duration(store.DefaultHistoryRetention()),
			Plugins:   store.DefaultHistoryPluginNames(),
		},
		Listeners: []Listener{},
	}
}

func (c Config) HistoryString() string { return strings.Join(c.History.Plugins, ",") }

func (c Config) View() View {
	listeners := c.Listeners
	if listeners == nil {
		listeners = []Listener{}
	}
	return View{
		Version:              c.Version,
		CollectorInterval:    c.Collector.Interval.Duration().String(),
		SmartRefreshInterval: c.Collector.SmartRefreshInterval.Duration().String(),
		DiskUsageCache:       c.Collector.DiskUsageCache.Duration().String(),
		HistoryRetention:     c.History.Retention.Duration().String(),
		History:              c.HistoryString(),
		Listeners:            listeners,
	}
}

// Load reads a strict YAML config. An absent file yields Default() and false.
func Load(path string) (Config, bool, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), false, nil
		}
		return Config{}, false, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg, err := decodeStrict(data)
	if err != nil {
		return Config{}, true, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := Validate(cfg); err != nil {
		return Config{}, true, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, true, nil
}

func decodeStrict(data []byte) (Config, error) {
	probe := yaml.NewDecoder(bytes.NewReader(data))
	var document any
	if err := probe.Decode(&document); err != nil {
		return Config{}, err
	}
	if document == nil {
		return Config{}, errors.New("YAML document is empty")
	}
	var extra any
	if err := probe.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return Config{}, fmt.Errorf("unexpected trailing YAML: %w", err)
		}
		return Config{}, errors.New("multiple YAML documents are not supported")
	}
	cfg := Default()
	if err := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict()).Decode(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.Listeners == nil {
		cfg.Listeners = []Listener{}
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := utils.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func SaveIfMissing(path string, cfg Config) (bool, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat config: %w", err)
	}
	if err := Save(path, cfg); err != nil {
		return false, err
	}
	return true, nil
}

var reservedSocketPaths = map[string]struct{}{
	monitoringapi.APISocketPath:     {},
	monitoringapi.ControlSocketPath: {},
}

func Validate(cfg Config) error {
	if cfg.Version != CurrentVersion {
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if cfg.Collector.Interval.Duration() <= 0 {
		return errors.New("collector.interval must be greater than zero")
	}
	if cfg.Collector.SmartRefreshInterval.Duration() <= 0 {
		return errors.New("collector.smart_refresh_interval must be greater than zero")
	}
	if cfg.History.Retention.Duration() <= 0 {
		return errors.New("history.retention must be greater than zero")
	}
	if _, err := store.ParseHistoryPlugins(cfg.HistoryString(), true); err != nil {
		return fmt.Errorf("history.plugins: %w", err)
	}
	return validateListeners(cfg.Listeners)
}

func validateListeners(listeners []Listener) error {
	seenNames := map[string]bool{}
	seenAddresses := map[string]bool{}
	for i, listener := range listeners {
		name := strings.ToLower(strings.TrimSpace(listener.Name))
		if name == "" {
			return fmt.Errorf("listeners[%d].name cannot be empty", i)
		}
		if name == "api" || name == "control" {
			return fmt.Errorf("listeners[%d].name %q is reserved", i, listener.Name)
		}
		if seenNames[name] {
			return fmt.Errorf("duplicate listener name %q", listener.Name)
		}
		seenNames[name] = true

		address := strings.TrimSpace(listener.Address)
		if address == "" {
			return fmt.Errorf("listeners[%d].address cannot be empty", i)
		}
		if app.IsListenDisabled(address) {
			return fmt.Errorf("listeners[%d].address cannot be disabled", i)
		}
		network, addr := app.SplitListenAddress(app.GetAddress(address))
		if network == "unix" {
			if _, reserved := reservedSocketPaths[addr]; reserved {
				return fmt.Errorf("listeners[%d].address %q is reserved for LinuxIO", i, address)
			}
		}
		key := network + ":" + addr
		if network == "tcp" {
			if host, port, err := net.SplitHostPort(addr); err == nil {
				key = "tcp:" + net.JoinHostPort(host, port)
			}
		}
		if seenAddresses[key] {
			return fmt.Errorf("duplicate listener address %q", listener.Address)
		}
		seenAddresses[key] = true

		seenPlugins := map[string]bool{}
		for _, plugin := range listener.Plugins {
			plugin = strings.ToLower(strings.TrimSpace(plugin))
			if !store.IsPluginName(plugin) {
				return fmt.Errorf("listeners[%d].plugins: unknown plugin %q", i, plugin)
			}
			if seenPlugins[plugin] {
				return fmt.Errorf("listeners[%d].plugins: duplicate plugin %q", i, plugin)
			}
			seenPlugins[plugin] = true
		}
	}
	return nil
}
```

The `monitoringapi` import requires the constants from Task 8; create `backend/monitoring/api/live.go` with only the two path constants now if Task 8 has not landed yet:

```go
package api

const (
	APISocketPath     = "/run/linuxio/monitoring/api.sock"
	ControlSocketPath = "/run/linuxio/monitoring/control.sock"
)
```

- [ ] **Step 4: Run the config tests**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/config/...
```

Expected: PASS. If `goccy/go-yaml` reports the unknown key with a different message, keep the assertion on the key name and adjust nothing else.

- [ ] **Step 5: Stage**

```bash
git add backend/monitoring/internal/config backend/monitoring/api
```

---

### Task 7: One-second live reuse and stale-baseline reseed

**Files:**
- Create: `backend/monitoring/internal/app/live_reuse.go`, `backend/monitoring/internal/app/live_reuse_test.go`
- Modify: `backend/monitoring/internal/app/agent.go` (fields), `backend/monitoring/internal/app/live_current.go` (callers), `backend/monitoring/internal/app/cpu.go`, `backend/monitoring/internal/app/network.go`, `backend/monitoring/internal/app/disk.go`, `backend/monitoring/internal/integration/docker/docker.go`, `backend/monitoring/internal/deltatracker/deltatracker.go`, plus their tests

**Interfaces:**
- Produces:
  - `func (a *App) liveCurrentData(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, time.Time, error)`; all live paths call it instead of `collectLiveCurrentData` directly and use the returned time as `captured_at`.
  - `const liveReuseWindow = time.Second`.
  - `func (t *DeltaTracker[K, V]) Clone() *DeltaTracker[K, V]`.
  - Reseed hooks: `reseedCPUFromCollector(key uint16)`, `(*networkManager).reseedFromCollector(key uint16)`, `(*fsManager).reseedFromCollector(key uint16)`, `(*docker.Manager).ReseedFromCollector(key, collectorKey uint16)`; each is called at the start of the manager's per-key update when `key != collectorDataKeyMs`.

- [ ] **Step 1: Write the failing reuse test**

`live_reuse_test.go`:

```go
package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestLiveCurrentDataReusesWithinWindow(t *testing.T) {
	a := &App{liveRuns: map[uint16]*liveRun{}}
	var collections atomic.Int32
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, error) {
		collections.Add(1)
		return &system.CombinedData{}, nil
	}

	first, firstAt, err := a.liveCurrentData(context.Background(), 1010, false, false)
	if err != nil {
		t.Fatal(err)
	}
	second, secondAt, err := a.liveCurrentData(context.Background(), 1010, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if collections.Load() != 1 || first != second || !firstAt.Equal(secondAt) {
		t.Fatalf("expected one shared collection, got %d", collections.Load())
	}

	a.liveRuns[1010].capturedAt = time.Now().Add(-2 * liveReuseWindow)
	if _, _, err := a.liveCurrentData(context.Background(), 1010, false, false); err != nil {
		t.Fatal(err)
	}
	if collections.Load() != 2 {
		t.Fatalf("stale sample must recollect, got %d collections", collections.Load())
	}
}

func TestLiveCurrentDataSharesInFlightCollection(t *testing.T) {
	a := &App{liveRuns: map[uint16]*liveRun{}}
	release := make(chan struct{})
	var collections atomic.Int32
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, error) {
		collections.Add(1)
		<-release
		return &system.CombinedData{}, nil
	}

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, _, err := a.liveCurrentData(context.Background(), 1001, false, true); err != nil {
				t.Error(err)
			}
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if collections.Load() != 1 {
		t.Fatalf("concurrent callers must share one collection, got %d", collections.Load())
	}
}

func TestLiveCurrentDataDoesNotReuseNarrowerSample(t *testing.T) {
	a := &App{liveRuns: map[uint16]*liveRun{}}
	var collections atomic.Int32
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, error) {
		collections.Add(1)
		return &system.CombinedData{}, nil
	}
	if _, _, err := a.liveCurrentData(context.Background(), 1001, false, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.liveCurrentData(context.Background(), 1001, false, true); err != nil {
		t.Fatal(err)
	}
	if collections.Load() != 2 {
		t.Fatalf("a sample without containers must not satisfy a request with containers, got %d", collections.Load())
	}
}
```

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run TestLiveCurrentData'
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement the reuse**

`live_reuse.go`:

```go
package app

import (
	"context"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// liveReuseWindow bounds how often a live key collects. Requests inside the
// window share the newest sample; captured_at reports that sample's time.
const liveReuseWindow = time.Second

type liveRun struct {
	done              chan struct{}
	includeDetails    bool
	includeContainers bool
	capturedAt        time.Time
	data              *system.CombinedData
	err               error
}

func (r *liveRun) covers(includeDetails, includeContainers bool) bool {
	return (r.includeDetails || !includeDetails) && (r.includeContainers || !includeContainers)
}

// liveCurrentData returns a live sample for key. It reuses a finished sample
// under liveReuseWindow old, joins an in-flight collection, and otherwise
// collects. The collector tick handoff in awaitCollectorSample still runs
// first in the callers, as before.
func (a *App) liveCurrentData(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, time.Time, error) {
	for {
		a.liveMu.Lock()
		if a.liveRuns == nil {
			a.liveRuns = map[uint16]*liveRun{}
		}
		run := a.liveRuns[key]
		if run != nil {
			select {
			case <-run.done:
				if run.err == nil && time.Since(run.capturedAt) < liveReuseWindow && run.covers(includeDetails, includeContainers) {
					a.liveMu.Unlock()
					return run.data, run.capturedAt, nil
				}
			default:
				a.liveMu.Unlock()
				select {
				case <-ctx.Done():
					return nil, time.Time{}, ctx.Err()
				case <-run.done:
				}
				if run.err == nil && run.covers(includeDetails, includeContainers) {
					return run.data, run.capturedAt, nil
				}
				continue
			}
		}
		run = &liveRun{done: make(chan struct{}), includeDetails: includeDetails, includeContainers: includeContainers}
		a.liveRuns[key] = run
		a.liveMu.Unlock()

		collect := a.collectLive
		if collect == nil {
			collect = a.collectLiveCurrentData
		}
		run.data, run.err = collect(ctx, key, includeDetails, includeContainers)
		run.capturedAt = time.Now()
		close(run.done)
		return run.data, run.capturedAt, run.err
	}
}

var _ = sync.Mutex{}
```

Add to `App` in `agent.go`:

```go
	liveMu      sync.Mutex
	liveRuns    map[uint16]*liveRun
	collectLive func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, error) // nil in production; tests inject
```

Remove the trailing `var _ = sync.Mutex{}` line once `sync` is used by the `App` field. In `live_current.go`:

- `collectSystemPlugin`: call `data, capturedAt, err := a.liveCurrentData(ctx, sampleKey, false, includeContainers)` and return `capturedAt.UTC().UnixMilli()` instead of `time.Now()`.
- `collectCurrentSystemBatch`: call `a.liveCurrentData(ctx, liveSampleKey(liveAllEndpoint), false, includeContainers)` and pass the returned time up through `CurrentPlugins` so `/api/v1/all` reports it (change the function to return that time when a system batch ran, else `time.Now()`).
- `SystemSummary`: call `a.liveCurrentData(ctx, liveSampleKey(liveSystemSummaryEndpoint), true, false)` and return its time.

- [ ] **Step 4: Run the reuse tests**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run TestLiveCurrentData'
```

Expected: PASS.

- [ ] **Step 5: Write the failing reseed tests**

Append to `cpu_test.go`:

```go
func TestCPUReseedsStaleLiveBaselineFromCollector(t *testing.T) {
	lastCpuTimes = map[uint16]cpu.TimesStat{}
	lastPerCoreCpuTimes = map[uint16][]cpu.TimesStat{}
	lastCpuSampleAt = map[uint16]time.Time{}

	lastCpuTimes[collectorDataKeyMs] = cpu.TimesStat{User: 100, Idle: 100}
	lastCpuSampleAt[collectorDataKeyMs] = time.Now()
	lastCpuTimes[1010] = cpu.TimesStat{User: 1, Idle: 1}
	lastCpuSampleAt[1010] = time.Now().Add(-10 * time.Minute)

	reseedCPUFromCollector(1010)
	if got := lastCpuTimes[1010]; got.User != 100 {
		t.Fatalf("stale baseline not reseeded: %+v", got)
	}

	lastCpuSampleAt[1010] = time.Now().Add(time.Second)
	lastCpuTimes[1010] = cpu.TimesStat{User: 5, Idle: 5}
	reseedCPUFromCollector(1010)
	if got := lastCpuTimes[1010]; got.User != 5 {
		t.Fatalf("fresh baseline must be kept: %+v", got)
	}
}
```

Append to `deltatracker_test.go`:

```go
func TestCloneCopiesBothGenerations(t *testing.T) {
	tracker := NewDeltaTracker[string, uint64]()
	tracker.Set("a", 10)
	tracker.Cycle()
	tracker.Set("a", 15)
	clone := tracker.Clone()
	tracker.Set("a", 99)
	if prev, _ := clone.Previous("a"); prev != 10 {
		t.Fatalf("previous = %d, want 10", prev)
	}
	if clone.Delta("a") != 5 {
		t.Fatalf("delta = %d, want 5", clone.Delta("a"))
	}
}
```

- [ ] **Step 6: Run to see them fail**

```bash
make test-go GO_TEST_PKGS='./monitoring/internal/app/... ./monitoring/internal/deltatracker/...' GO_TEST_FLAGS='-run "TestCPUReseeds|TestCloneCopies"'
```

Expected: FAIL to compile.

- [ ] **Step 7: Implement the reseeds**

`deltatracker.go`, add:

```go
// Clone returns an independent copy of both generations.
func (t *DeltaTracker[K, V]) Clone() *DeltaTracker[K, V] {
	t.RLock()
	defer t.RUnlock()
	return &DeltaTracker[K, V]{current: maps.Clone(t.current), previous: maps.Clone(t.previous)}
}
```

`cpu.go`: add `var lastCpuSampleAt = make(map[uint16]time.Time)`; in `getCpuMetrics` and `getPerCoreCpuUsage`, after reading `times`, call `reseedCPUFromCollector(cacheTimeMs)` before the existing fallback, and set `lastCpuSampleAt[cacheTimeMs] = time.Now()` where the last times are stored. Add:

```go
// reseedCPUFromCollector replaces a live key's baseline with the collector's
// when the live baseline predates the last collector tick, so a request after
// idle averages over at most one collector interval.
func reseedCPUFromCollector(cacheTimeMs uint16) {
	if cacheTimeMs == collectorDataKeyMs {
		return
	}
	collectorAt, ok := lastCpuSampleAt[collectorDataKeyMs]
	if !ok {
		return
	}
	if at, ok := lastCpuSampleAt[cacheTimeMs]; ok && !at.Before(collectorAt) {
		return
	}
	if times, ok := lastCpuTimes[collectorDataKeyMs]; ok {
		lastCpuTimes[cacheTimeMs] = times
	}
	if perCore, ok := lastPerCoreCpuTimes[collectorDataKeyMs]; ok {
		lastPerCoreCpuTimes[cacheTimeMs] = append([]cpu.TimesStat(nil), perCore...)
	}
	lastCpuSampleAt[cacheTimeMs] = collectorAt
}
```

`network.go`: at the top of `updateNetworkStats` after the counters are read, call `m.reseedFromCollector(cacheTimeMs)`:

```go
func (m *networkManager) reseedFromCollector(cacheTimeMs uint16) {
	if cacheTimeMs == collectorDataKeyMs {
		return
	}
	collector, ok := m.netIoStats[collectorDataKeyMs]
	if !ok || collector.Time.IsZero() {
		return
	}
	if live, ok := m.netIoStats[cacheTimeMs]; ok && !live.Time.Before(collector.Time) {
		return
	}
	m.netIoStats[cacheTimeMs] = collector
	if tracker := m.netInterfaceDeltaTrackers[collectorDataKeyMs]; tracker != nil {
		m.netInterfaceDeltaTrackers[cacheTimeMs] = tracker.Clone()
	}
}
```

`disk.go`: at the top of `applyDiskIoCounters`, call `m.reseedFromCollector(cacheTimeMs)`:

```go
func (m *fsManager) reseedFromCollector(cacheTimeMs uint16) {
	if cacheTimeMs == collectorDataKeyMs {
		return
	}
	collector := m.diskPrev[collectorDataKeyMs]
	if len(collector) == 0 {
		return
	}
	var collectorAt, liveAt time.Time
	for _, prev := range collector {
		if prev.at.After(collectorAt) {
			collectorAt = prev.at
		}
	}
	for _, prev := range m.diskPrev[cacheTimeMs] {
		if prev.at.After(liveAt) {
			liveAt = prev.at
		}
	}
	if !liveAt.Before(collectorAt) {
		return
	}
	m.diskPrev[cacheTimeMs] = maps.Clone(collector)
}
```

`integration/docker/docker.go`: at the top of `GetStats`, after the container list is decoded, call `dm.ReseedFromCollector(cacheTimeMs, collectorKey)` where `collectorKey` is a new `Manager` field set by the app (`dockerintegration.NewManager(ctx, onPodman)` gains `SetCollectorKey(key uint16)`; `agent.go` calls `app.dockerManager.SetCollectorKey(collectorDataKeyMs)` after construction):

```go
// ReseedFromCollector copies the collector's per-container baselines onto a
// live key whose newest read predates the collector's newest read.
func (dm *Manager) ReseedFromCollector(cacheTimeMs, collectorKey uint16) {
	if cacheTimeMs == collectorKey {
		return
	}
	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()
	collectorTimes := dm.lastNetworkReadTime[collectorKey]
	if len(collectorTimes) == 0 {
		return
	}
	var collectorAt, liveAt time.Time
	for _, at := range collectorTimes {
		if at.After(collectorAt) {
			collectorAt = at
		}
	}
	for _, at := range dm.lastNetworkReadTime[cacheTimeMs] {
		if at.After(liveAt) {
			liveAt = at
		}
	}
	if !liveAt.Before(collectorAt) {
		return
	}
	dm.lastCpuContainer[cacheTimeMs] = maps.Clone(dm.lastCpuContainer[collectorKey])
	dm.lastCpuSystem[cacheTimeMs] = maps.Clone(dm.lastCpuSystem[collectorKey])
	dm.lastNetworkReadTime[cacheTimeMs] = maps.Clone(collectorTimes)
	if tracker := dm.networkSentTrackers[collectorKey]; tracker != nil {
		dm.networkSentTrackers[cacheTimeMs] = tracker.Clone()
	}
	if tracker := dm.networkRecvTrackers[collectorKey]; tracker != nil {
		dm.networkRecvTrackers[cacheTimeMs] = tracker.Clone()
	}
}
```

If `lastCpuContainer` and friends are guarded by a different mutex in the copied code, use that mutex instead of `containerStatsMutex`; read the surrounding accessors (`initializeCpuTracking`, `getCpuPreviousValues`) and match them.

- [ ] **Step 8: Add reseed tests for network, disk and docker**

Append to `network_test.go`:

```go
func TestNetworkReseedsStaleLiveKey(t *testing.T) {
	m := newNetworkManager()
	m.netIoStats[collectorDataKeyMs] = system.NetIoStats{BytesSent: 500, BytesRecv: 700, Time: time.Now()}
	tracker := deltatracker.NewDeltaTracker[string, uint64]()
	tracker.Set("eth0up", 500)
	tracker.Cycle()
	m.netInterfaceDeltaTrackers[collectorDataKeyMs] = tracker
	m.netIoStats[1010] = system.NetIoStats{BytesSent: 1, BytesRecv: 1, Time: time.Now().Add(-time.Hour)}

	m.reseedFromCollector(1010)
	if m.netIoStats[1010].BytesSent != 500 {
		t.Fatalf("baseline not reseeded: %+v", m.netIoStats[1010])
	}
	if prev, ok := m.netInterfaceDeltaTrackers[1010].Previous("eth0up"); !ok || prev != 500 {
		t.Fatalf("tracker not cloned: %d %v", prev, ok)
	}
}
```

Append to `disk_test.go`:

```go
func TestDiskReseedsStaleLiveKey(t *testing.T) {
	m := newFsManager()
	m.diskPrev[collectorDataKeyMs] = map[string]prevDisk{"sda": {readBytes: 10, at: time.Now()}}
	m.diskPrev[1010] = map[string]prevDisk{"sda": {readBytes: 1, at: time.Now().Add(-time.Hour)}}
	m.reseedFromCollector(1010)
	if m.diskPrev[1010]["sda"].readBytes != 10 {
		t.Fatalf("not reseeded: %+v", m.diskPrev[1010])
	}
}
```

Append to `integration/docker/docker_test.go`:

```go
func TestReseedFromCollectorCopiesBaselines(t *testing.T) {
	dm := NewManager(context.Background(), func() {})
	dm.initializeCpuTracking(60000)
	dm.initializeCpuTracking(1010)
	dm.lastCpuContainer[60000]["abc"] = 900
	dm.lastNetworkReadTime[60000] = map[string]time.Time{"abc": time.Now()}
	dm.lastCpuContainer[1010]["abc"] = 1
	dm.lastNetworkReadTime[1010] = map[string]time.Time{"abc": time.Now().Add(-time.Hour)}

	dm.ReseedFromCollector(1010, 60000)
	if dm.lastCpuContainer[1010]["abc"] != 900 {
		t.Fatalf("not reseeded: %v", dm.lastCpuContainer[1010])
	}
}
```

If `NewManager` in the copied code needs a reachable Docker socket to construct, use the package's existing test constructor instead (search `docker_testing_test.go` for how tests build a `Manager`).

- [ ] **Step 9: Run all app, docker and deltatracker tests**

```bash
make test-go GO_TEST_PKGS='./monitoring/internal/app/... ./monitoring/internal/integration/docker/... ./monitoring/internal/deltatracker/...'
```

Expected: PASS.

- [ ] **Step 10: Stage**

```bash
git add backend/monitoring/internal
```

---

### Task 8: `api.Live`, byte-precise fields, per-device disk rates, `/api/v1/live`

**Files:**
- Create: `backend/monitoring/api/live.go`, `backend/monitoring/internal/app/live_api.go`, `backend/monitoring/internal/app/live_api_test.go`, `backend/monitoring/internal/app/disk_devices.go`, `backend/monitoring/internal/app/disk_devices_test.go`
- Modify: `backend/monitoring/internal/domain/system/system.go` (raw byte fields), `backend/monitoring/internal/app/system.go` (`updateMemoryStats`), `backend/monitoring/internal/app/agent.go` (telemetry memo, fs manager call), `backend/monitoring/internal/app/runtime.go` (`collectAndPersist`, `apiServer`), `backend/monitoring/internal/api/http/server.go` (route), `backend/monitoring/internal/api/http/server_test.go`

**Interfaces:**
- Produces `backend/monitoring/api/live.go`:

```go
package api

const (
	APISocketPath     = "/run/linuxio/monitoring/api.sock"
	ControlSocketPath = "/run/linuxio/monitoring/control.sock"
	RouteLive         = "/api/v1/live"
)

type Live struct {
	CapturedAtMs  int64                    `json:"captured_at_ms"`
	UptimeSeconds uint64                   `json:"uptime_seconds"`
	CPU           LiveCPU                  `json:"cpu"`
	Memory        LiveMemory               `json:"memory"`
	Disks         map[string]LiveDiskRates `json:"disks"`
	DiskIO        LiveDiskRates            `json:"disk_io"`
	Interfaces    map[string]LiveInterface `json:"interfaces"`
	Containers    LiveContainers           `json:"containers"`
}

type LiveCPU struct {
	Percent        float64          `json:"percent"`
	PerCorePercent []float64        `json:"per_core_percent"`
	Breakdown      LiveCPUBreakdown `json:"breakdown"`
	LoadAverage    [3]float64       `json:"load_average"`
}

type LiveCPUBreakdown struct {
	User   float64 `json:"user"`
	System float64 `json:"system"`
	IOWait float64 `json:"iowait"`
	Steal  float64 `json:"steal"`
	Idle   float64 `json:"idle"`
}

type LiveMemory struct {
	TotalBytes      uint64 `json:"total_bytes"`
	UsedBytes       uint64 `json:"used_bytes"`
	AvailableBytes  uint64 `json:"available_bytes"`
	FreeBytes       uint64 `json:"free_bytes"`
	CachedBytes     uint64 `json:"cached_bytes"`
	BuffersBytes    uint64 `json:"buffers_bytes"`
	SharedBytes     uint64 `json:"shared_bytes"`
	SwapTotalBytes  uint64 `json:"swap_total_bytes"`
	SwapFreeBytes   uint64 `json:"swap_free_bytes"`
	ZFSArcBytes     uint64 `json:"zfs_arc_bytes"`
	DockerUsedBytes uint64 `json:"docker_used_bytes"`
}

type LiveDiskRates struct {
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
	ReadOpsPerSec    float64 `json:"read_ops_per_sec"`
	WriteOpsPerSec   float64 `json:"write_ops_per_sec"`
}

type LiveInterface struct {
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
	RxBytesTotal  uint64  `json:"rx_bytes_total"`
	TxBytesTotal  uint64  `json:"tx_bytes_total"`
}

type LiveContainers struct {
	CapturedAtMs int64           `json:"captured_at_ms"`
	Items        []LiveContainer `json:"items"`
}

// LiveContainer carries CPU in Docker's multi-core convention (a container
// using two full cores reports 200).
type LiveContainer struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	CPUPercent            float64  `json:"cpu_percent"`
	MemoryBytes           uint64   `json:"memory_bytes"`
	RxBytesPerSec         float64  `json:"rx_bytes_per_sec"`
	TxBytesPerSec         float64  `json:"tx_bytes_per_sec"`
	BlockReadBytesPerSec  *float64 `json:"block_read_bytes_per_sec,omitempty"`
	BlockWriteBytesPerSec *float64 `json:"block_write_bytes_per_sec,omitempty"`
}
```

- Produces in `domain/system`: `Stats.MemoryBytes MemoryBytes` (`json:"-" cbor:"-"`) with the eleven uint64 fields above minus `DockerUsedBytes`, and `Stats.DiskDevices map[string]DiskDeviceRates` (`json:"-" cbor:"-"`) with `ReadBytesPerSec, WriteBytesPerSec, ReadOpsPerSec, WriteOpsPerSec float64`.
- Produces in `app`: `func (a *App) Live(ctx context.Context) (api.Live, error)` and pure `func buildLive(data *system.CombinedData, capturedAt time.Time, threads int, telemetry []container.Telemetry, telemetryAt time.Time, freshFor time.Duration) api.Live`.
- Produces in `httpapi`: `Options.Live func(context.Context) (api.Live, error)`; route `GET /api/v1/live` on metrics listeners, sections filtered by the listener allowlist.

- [ ] **Step 1: Write `api/live.go`** exactly as in the interface block above (replace the two-constant stub from Task 6).

- [ ] **Step 2: Add raw byte fields to `system.Stats`**

In `domain/system/system.go` add to `Stats`:

```go
	// MemoryBytes and DiskDevices are byte-precise views for the LinuxIO live
	// payload. They are never persisted or serialised as plugin data.
	MemoryBytes MemoryBytes                `json:"-" cbor:"-"`
	DiskDevices map[string]DiskDeviceRates `json:"-" cbor:"-"`
```

and the types:

```go
type MemoryBytes struct {
	Total, Used, Available, Free, Cached, Buffers, Shared, SwapTotal, SwapFree, ZFSArc uint64
}

type DiskDeviceRates struct {
	ReadBytesPerSec, WriteBytesPerSec, ReadOpsPerSec, WriteOpsPerSec float64
}
```

In `app/system.go` `updateMemoryStats`, after `v` is read and before any `htop`/ZFS adjustment of `v.Used`, record the raw values, then after the ZFS block record `Used` and `ZFSArc`:

```go
	systemStats.MemoryBytes = system.MemoryBytes{
		Total: v.Total, Available: v.Available, Free: v.Free, Cached: v.Cached,
		Buffers: v.Buffers, Shared: v.Shared, SwapTotal: v.SwapTotal, SwapFree: v.SwapFree,
	}
	// ... existing htop / ZFS adjustments ...
	systemStats.MemoryBytes.Used = v.Used
	systemStats.MemoryBytes.ZFSArc = arcBytes // the arcSize read in the ZFS block, 0 when not ZFS
```

- [ ] **Step 3: Write the failing per-device disk test**

`disk_devices_test.go`:

```go
package app

import (
	"context"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestBlockDeviceRatesFilterAndDelta(t *testing.T) {
	m := newFsManager()
	physical := func(string) bool { return true }
	now := time.Now()
	first := map[string]disk.IOCountersStat{
		"sda":   {ReadBytes: 1000, WriteBytes: 0, ReadCount: 10, WriteCount: 0},
		"loop0": {ReadBytes: 5},
	}
	var stats system.Stats
	m.applyBlockDeviceCounters(1010, &stats, first, now, func(name string) bool { return name != "loop0" && physical(name) })
	if len(stats.DiskDevices) != 0 {
		t.Fatalf("first sample must carry no rates, got %v", stats.DiskDevices)
	}
	second := map[string]disk.IOCountersStat{
		"sda":   {ReadBytes: 3000, WriteBytes: 500, ReadCount: 30, WriteCount: 5},
		"loop0": {ReadBytes: 50},
	}
	stats = system.Stats{}
	m.applyBlockDeviceCounters(1010, &stats, second, now.Add(2*time.Second), func(name string) bool { return name != "loop0" })
	got := stats.DiskDevices["sda"]
	if got.ReadBytesPerSec != 1000 || got.WriteBytesPerSec != 250 || got.ReadOpsPerSec != 10 || got.WriteOpsPerSec != 2.5 {
		t.Fatalf("rates = %+v", got)
	}
	if _, ok := stats.DiskDevices["loop0"]; ok {
		t.Fatal("loop device must be filtered")
	}
}

func TestIsPhysicalBlockDeviceRejectsVirtualNames(t *testing.T) {
	for _, name := range []string{"loop0", "ram1", "zram0", "dm-0", "md0", "sr0", "fd0", "", "a/b"} {
		if isPhysicalBlockDevice(context.Background(), name, func(string) bool { return true }) {
			t.Fatalf("%q must be rejected", name)
		}
	}
	if !isPhysicalBlockDevice(context.Background(), "nvme0n1", func(string) bool { return true }) {
		t.Fatal("nvme0n1 must pass when sysfs says it has a device")
	}
}
```

- [ ] **Step 4: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run "TestBlockDeviceRates|TestIsPhysicalBlockDevice"'
```

Expected: FAIL to compile.

- [ ] **Step 5: Implement `disk_devices.go`**

```go
package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// Per-block-device rates for the LinuxIO live payload. Keyed by interval key
// like every other delta so live and collector samples never share a baseline.

func sysBlockHasDevice(name string) bool {
	_, err := os.Stat(filepath.Join("/sys/block", name, "device"))
	return err == nil
}

// isPhysicalBlockDevice mirrors the filter the bridge used for disk
// throughput: skip virtual and partition-like names, keep devices sysfs
// reports with a physical device node.
func isPhysicalBlockDevice(ctx context.Context, name string, hasDevice func(string) bool) bool {
	if ctx.Err() != nil || name == "" || strings.Contains(name, "/") {
		return false
	}
	for _, prefix := range []string{"loop", "ram", "zram", "dm-", "md", "sr", "fd"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return hasDevice(name)
}

func (m *fsManager) updateBlockDeviceRates(ctx context.Context, cacheTimeMs uint16, systemStats *system.Stats) error {
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return nil
	}
	m.applyBlockDeviceCounters(cacheTimeMs, systemStats, counters, time.Now(), func(name string) bool {
		return isPhysicalBlockDevice(ctx, name, sysBlockHasDevice)
	})
	return nil
}

func (m *fsManager) applyBlockDeviceCounters(cacheTimeMs uint16, systemStats *system.Stats, counters map[string]disk.IOCountersStat, now time.Time, keep func(string) bool) {
	if m.devicePrev == nil {
		m.devicePrev = map[uint16]map[string]prevDisk{}
	}
	if m.devicePrev[cacheTimeMs] == nil {
		m.devicePrev[cacheTimeMs] = map[string]prevDisk{}
	}
	prevByName := m.devicePrev[cacheTimeMs]
	systemStats.DiskDevices = map[string]system.DiskDeviceRates{}
	for name, counter := range counters {
		if !keep(name) {
			continue
		}
		prev, hasPrev := prevByName[name]
		prevByName[name] = prevDiskFromCounter(counter, now)
		if !hasPrev {
			continue
		}
		seconds := now.Sub(prev.at).Seconds()
		if seconds <= 0 || counter.ReadBytes < prev.readBytes || counter.WriteBytes < prev.writeBytes {
			continue
		}
		systemStats.DiskDevices[name] = system.DiskDeviceRates{
			ReadBytesPerSec:  float64(counter.ReadBytes-prev.readBytes) / seconds,
			WriteBytesPerSec: float64(counter.WriteBytes-prev.writeBytes) / seconds,
			ReadOpsPerSec:    float64(counter.ReadCount-prev.readCount) / seconds,
			WriteOpsPerSec:   float64(counter.WriteCount-prev.writeCount) / seconds,
		}
	}
}
```

Add `devicePrev map[uint16]map[string]prevDisk` to `fsManager`, and extend `reseedFromCollector` from Task 7 to clone `m.devicePrev[collectorDataKeyMs]` under the same staleness rule. Call `m.fsManager.updateBlockDeviceRates(ctx, cacheTimeMs, &systemStats)` in `getSystemStats` right after `updateDiskIo`.

- [ ] **Step 6: Write the failing builder test**

`live_api_test.go`:

```go
package app

import (
	"testing"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestBuildLiveMapsSample(t *testing.T) {
	at := time.UnixMilli(1_700_000_000_000)
	data := &system.CombinedData{
		Stats: system.Stats{
			Cpu:           12.5,
			CpuBreakdown:  []float64{5, 4, 1, 0, 90},
			CpuCoresUsage: system.Uint8Slice{10, 15},
			LoadAvg:       [3]float64{1, 2, 3},
			MemoryBytes:   system.MemoryBytes{Total: 16 << 30, Used: 8 << 30, ZFSArc: 1 << 30},
			DiskDevices: map[string]system.DiskDeviceRates{
				"sda":     {ReadBytesPerSec: 100, WriteBytesPerSec: 50, ReadOpsPerSec: 1, WriteOpsPerSec: 2},
				"nvme0n1": {ReadBytesPerSec: 300, WriteBytesPerSec: 0},
			},
			NetworkInterfaces: map[string][4]uint64{"eth0": {1000, 2000, 30000, 40000}},
		},
		Info: system.Info{Uptime: 4242},
		Containers: []*container.Stats{
			{Id: "abc123def456", Name: "web", Cpu: 25, Mem: 512, Bandwidth: [2]uint64{7, 9}},
		},
	}
	telemetry := []container.Telemetry{{ID: "abc123def456", DiskReadBytesPerSecond: 11, DiskWriteBytesPerSecond: 13}}

	live := buildLive(data, at, 4, telemetry, at.Add(-30*time.Second), 3*time.Minute)

	if live.CapturedAtMs != at.UnixMilli() || live.UptimeSeconds != 4242 {
		t.Fatalf("header = %+v", live)
	}
	if live.CPU.Percent != 12.5 || live.CPU.Breakdown.Idle != 90 || len(live.CPU.PerCorePercent) != 2 || live.CPU.PerCorePercent[1] != 15 {
		t.Fatalf("cpu = %+v", live.CPU)
	}
	if live.Memory.TotalBytes != 16<<30 || live.Memory.ZFSArcBytes != 1<<30 || live.Memory.DockerUsedBytes != 512<<20 {
		t.Fatalf("memory = %+v", live.Memory)
	}
	if live.DiskIO.ReadBytesPerSec != 400 || live.Disks["sda"].WriteOpsPerSec != 2 {
		t.Fatalf("disks = %+v %+v", live.DiskIO, live.Disks)
	}
	eth := live.Interfaces["eth0"]
	if eth.TxBytesPerSec != 1000 || eth.RxBytesPerSec != 2000 || eth.TxBytesTotal != 30000 || eth.RxBytesTotal != 40000 {
		t.Fatalf("eth0 = %+v", eth)
	}
	if len(live.Containers.Items) != 1 {
		t.Fatalf("containers = %+v", live.Containers)
	}
	ctr := live.Containers.Items[0]
	if ctr.CPUPercent != 100 || ctr.MemoryBytes != 512<<20 || ctr.TxBytesPerSec != 7 || ctr.RxBytesPerSec != 9 {
		t.Fatalf("container = %+v", ctr)
	}
	if ctr.BlockReadBytesPerSec == nil || *ctr.BlockReadBytesPerSec != 11 || *ctr.BlockWriteBytesPerSec != 13 {
		t.Fatalf("telemetry not attached: %+v", ctr)
	}

	stale := buildLive(data, at, 4, telemetry, at.Add(-10*time.Minute), 3*time.Minute)
	if stale.Containers.Items[0].BlockReadBytesPerSec != nil {
		t.Fatal("stale telemetry must leave block rates nil")
	}
	var _ monitoringapi.Live = stale
}
```

- [ ] **Step 7: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run TestBuildLiveMapsSample'
```

Expected: FAIL to compile.

- [ ] **Step 8: Implement `live_api.go`**

```go
package app

import (
	"context"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

const liveLiveEndpoint = "live"

const bytesPerMiB = 1024 * 1024

// Live returns the LinuxIO-shaped live payload. Same request semantics as any
// live key: the collector handoff first, then the one-second reuse.
func (a *App) Live(ctx context.Context) (monitoringapi.Live, error) {
	if err := ctx.Err(); err != nil {
		return monitoringapi.Live{}, err
	}
	data, capturedAt, err := a.liveCurrentData(ctx, liveSampleKey(liveLiveEndpoint), true, true)
	if err != nil {
		return monitoringapi.Live{}, err
	}
	threads := 0
	if data.Details != nil {
		threads = data.Details.Threads
	}
	telemetry, telemetryAt := a.lastTelemetry()
	return buildLive(data, capturedAt, threads, telemetry, telemetryAt, 3*a.CollectorInterval()), nil
}

func (a *App) lastTelemetry() ([]container.Telemetry, time.Time) {
	a.telemetryMu.RLock()
	defer a.telemetryMu.RUnlock()
	return a.telemetry, a.telemetryAt
}

func (a *App) rememberTelemetry(items []container.Telemetry, at time.Time) {
	a.telemetryMu.Lock()
	a.telemetry = items
	a.telemetryAt = at
	a.telemetryMu.Unlock()
}

func buildLive(data *system.CombinedData, capturedAt time.Time, threads int, telemetry []container.Telemetry, telemetryAt time.Time, freshFor time.Duration) monitoringapi.Live {
	stats := data.Stats
	live := monitoringapi.Live{
		CapturedAtMs:  capturedAt.UTC().UnixMilli(),
		UptimeSeconds: data.Info.Uptime,
		CPU: monitoringapi.LiveCPU{
			Percent:        stats.Cpu,
			PerCorePercent: make([]float64, 0, len(stats.CpuCoresUsage)),
			LoadAverage:    stats.LoadAvg,
		},
		Memory: monitoringapi.LiveMemory{
			TotalBytes:     stats.MemoryBytes.Total,
			UsedBytes:      stats.MemoryBytes.Used,
			AvailableBytes: stats.MemoryBytes.Available,
			FreeBytes:      stats.MemoryBytes.Free,
			CachedBytes:    stats.MemoryBytes.Cached,
			BuffersBytes:   stats.MemoryBytes.Buffers,
			SharedBytes:    stats.MemoryBytes.Shared,
			SwapTotalBytes: stats.MemoryBytes.SwapTotal,
			SwapFreeBytes:  stats.MemoryBytes.SwapFree,
			ZFSArcBytes:    stats.MemoryBytes.ZFSArc,
		},
		Disks:      map[string]monitoringapi.LiveDiskRates{},
		Interfaces: map[string]monitoringapi.LiveInterface{},
		Containers: monitoringapi.LiveContainers{CapturedAtMs: capturedAt.UTC().UnixMilli(), Items: []monitoringapi.LiveContainer{}},
	}
	for _, core := range stats.CpuCoresUsage {
		live.CPU.PerCorePercent = append(live.CPU.PerCorePercent, float64(core))
	}
	if len(stats.CpuBreakdown) == 5 {
		live.CPU.Breakdown = monitoringapi.LiveCPUBreakdown{
			User: stats.CpuBreakdown[0], System: stats.CpuBreakdown[1], IOWait: stats.CpuBreakdown[2],
			Steal: stats.CpuBreakdown[3], Idle: stats.CpuBreakdown[4],
		}
	}
	for name, rates := range stats.DiskDevices {
		live.Disks[name] = monitoringapi.LiveDiskRates(rates)
		live.DiskIO.ReadBytesPerSec += rates.ReadBytesPerSec
		live.DiskIO.WriteBytesPerSec += rates.WriteBytesPerSec
		live.DiskIO.ReadOpsPerSec += rates.ReadOpsPerSec
		live.DiskIO.WriteOpsPerSec += rates.WriteOpsPerSec
	}
	for name, values := range stats.NetworkInterfaces {
		live.Interfaces[name] = monitoringapi.LiveInterface{
			TxBytesPerSec: float64(values[0]), RxBytesPerSec: float64(values[1]),
			TxBytesTotal: values[2], RxBytesTotal: values[3],
		}
	}

	telemetryFresh := !telemetryAt.IsZero() && capturedAt.Sub(telemetryAt) <= freshFor
	blockRates := map[string]container.Telemetry{}
	if telemetryFresh {
		for _, item := range telemetry {
			blockRates[item.ID] = item
		}
	}
	cpuMultiplier := float64(max(threads, 1))
	for _, ctr := range data.Containers {
		if ctr == nil {
			continue
		}
		memBytes := uint64(ctr.Mem * bytesPerMiB)
		live.Memory.DockerUsedBytes += memBytes
		item := monitoringapi.LiveContainer{
			ID: ctr.Id, Name: ctr.Name,
			CPUPercent:    ctr.Cpu * cpuMultiplier,
			MemoryBytes:   memBytes,
			TxBytesPerSec: float64(ctr.Bandwidth[0]), RxBytesPerSec: float64(ctr.Bandwidth[1]),
		}
		if rec, ok := blockRates[ctr.Id]; ok {
			read, write := float64(rec.DiskReadBytesPerSecond), float64(rec.DiskWriteBytesPerSecond)
			item.BlockReadBytesPerSec, item.BlockWriteBytesPerSec = &read, &write
		}
		live.Containers.Items = append(live.Containers.Items, item)
	}
	return live
}
```

`LiveDiskRates(rates)` compiles because `system.DiskDeviceRates` and `api.LiveDiskRates` have identical field sets; if the linter objects to the conversion, assign field by field. Add to `App` in `agent.go`:

```go
	telemetryMu sync.RWMutex
	telemetry   []container.Telemetry
	telemetryAt time.Time
```

In `runtime.go` `collectAndPersist`, after the store write: `a.rememberTelemetry(data.ContainerTelemetry, time.UnixMilli(capturedAt))`. Add `liveLiveEndpoint` to `liveSampleKey` with its own constant `liveLiveSampleKey = uint16(1_002)`. In `apiServer` add `Live: a.Live,`.

- [ ] **Step 9: Mount the route with allowlist filtering**

In `server.go` add `live func(context.Context) (monitoringapi.Live, error)` to `Server`, `Live` to `Options`, and inside the metrics branch of `HandlerFor`: `mux.HandleFunc(monitoringapi.RouteLive, s.handleLive(plugins))`.

```go
// liveSectionPlugins maps live payload sections to the plugin allowlist a
// configured listener may restrict.
var liveSectionPlugins = map[string][]string{
	"cpu":        {"cpu"},
	"memory":     {"mem", "swap"},
	"disks":      {"diskio"},
	"interfaces": {"network"},
	"containers": {"containers", "container_telemetry"},
}

func (s *Server) handleLive(plugins []string) http.HandlerFunc {
	allowed := map[string]bool{}
	for _, name := range plugins {
		allowed[name] = true
	}
	permitted := func(section string) bool {
		if plugins == nil {
			return true
		}
		for _, plugin := range liveSectionPlugins[section] {
			if allowed[plugin] {
				return true
			}
		}
		return false
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		if s.live == nil {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := requestContext(r)
		defer cancel()
		live, err := s.live(ctx)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !permitted("cpu") {
			live.CPU = monitoringapi.LiveCPU{PerCorePercent: []float64{}}
		}
		if !permitted("memory") {
			live.Memory = monitoringapi.LiveMemory{}
		}
		if !permitted("disks") {
			live.Disks, live.DiskIO = map[string]monitoringapi.LiveDiskRates{}, monitoringapi.LiveDiskRates{}
		}
		if !permitted("interfaces") {
			live.Interfaces = map[string]monitoringapi.LiveInterface{}
		}
		if !permitted("containers") {
			live.Containers = monitoringapi.LiveContainers{Items: []monitoringapi.LiveContainer{}}
		}
		writeJSON(w, http.StatusOK, live)
	}
}
```

Append to `server_test.go`:

```go
func TestLiveRouteFiltersSectionsByAllowlist(t *testing.T) {
	srv := NewServer(Options{Live: func(context.Context) (monitoringapi.Live, error) {
		return monitoringapi.Live{
			CPU:        monitoringapi.LiveCPU{Percent: 50},
			Interfaces: map[string]monitoringapi.LiveInterface{"eth0": {RxBytesPerSec: 1}},
		}, nil
	}})
	handler := srv.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, []string{"cpu"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, monitoringapi.RouteLive, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var live monitoringapi.Live
	if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	if live.CPU.Percent != 50 || len(live.Interfaces) != 0 {
		t.Fatalf("filtering failed: %+v", live)
	}
}
```

- [ ] **Step 10: Run**

```bash
make test-go GO_TEST_PKGS='./monitoring/...'
```

Expected: PASS for every package except `internal/daemon` (Task 9) and `internal/cli` (absent until Task 9).

- [ ] **Step 11: Stage**

```bash
git add backend/monitoring
```

---

### Task 9: Daemon package, command executor, CLI entry, unit

**Files:**
- Create: `backend/monitoring/internal/daemon/daemon.go`, `backend/monitoring/internal/daemon/daemon_test.go`, `backend/monitoring/internal/cli/main.go`, `backend/monitoring/internal/cli/main_test.go`, `backend/monitoring/main.go`, `packaging/systemd/linuxio-monitoring.service`, `packaging/etc/linuxio/monitoring/config.yaml`
- Modify: `backend/monitoring/internal/daemon/command.go`, `backend/monitoring/internal/daemon/command_test.go`, `backend/monitoring/internal/app/runtime.go` (`RunOptions.DiskUsageCache`, `ReloadOptions.DiskUsageCache`), `backend/monitoring/internal/app/disk.go` (`setDiskUsageCache`), `backend/monitoring/internal/app/agent.go` (`New` data dir)

**Interfaces:**
- Produces:
  - `daemon.Run(ctx context.Context, configPath string) error`.
  - `daemon.DataDir = "/var/lib/linuxio/monitoring"`.
  - `daemon.Listeners(cfg config.Config) []app.ListenerOptions` (two fixed sockets plus configured, metrics only).
  - `daemon.NewCommandExecutor(a *app.App, configPath string) httpapi.CommandExecutor`.
  - `cli.Main(args []string) int`.
  - `app.RunOptions.DiskUsageCache time.Duration`, `app.ReloadOptions.DiskUsageCache time.Duration`, `func (m *fsManager) setDiskUsageCache(d time.Duration)`.

- [ ] **Step 1: Write the failing listener-shape test**

`daemon_test.go`:

```go
package daemon

import (
	"testing"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
)

func TestListenersAlwaysIncludeFixedSockets(t *testing.T) {
	cfg := config.Default()
	cfg.Listeners = []config.Listener{{Name: "homepage", Address: "0.0.0.0:45876", Plugins: []string{"cpu"}}}
	listeners := Listeners(cfg)
	if len(listeners) != 3 {
		t.Fatalf("listeners = %+v", listeners)
	}
	api, control, homepage := listeners[0], listeners[1], listeners[2]
	if api.Address != "unix:"+monitoringapi.APISocketPath || api.Mode != 0o666 || api.RootOnly || api.Plugins != nil || len(api.APIs) != 1 || api.APIs[0] != "metrics" {
		t.Fatalf("api = %+v", api)
	}
	if control.Address != "unix:"+monitoringapi.ControlSocketPath || control.Mode != 0o600 || !control.RootOnly || len(control.APIs) != 2 {
		t.Fatalf("control = %+v", control)
	}
	if homepage.Address != "0.0.0.0:45876" || homepage.RootOnly || len(homepage.APIs) != 1 || homepage.APIs[0] != "metrics" || homepage.Plugins[0] != "cpu" {
		t.Fatalf("homepage = %+v", homepage)
	}
}
```

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/daemon/... GO_TEST_FLAGS='-run TestListenersAlwaysIncludeFixedSockets'
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `daemon.go`**

```go
// Package daemon runs linuxio-monitoring: config, listeners, and the app.
package daemon

import (
	"context"
	"fmt"
	"log/slog"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/app"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
)

const DataDir = "/var/lib/linuxio/monitoring"

// Listeners returns the two fixed LinuxIO sockets followed by the configured
// read-only listeners. Commands are never served off the control socket.
func Listeners(cfg config.Config) []app.ListenerOptions {
	out := []app.ListenerOptions{
		{Name: "api", Address: "unix:" + monitoringapi.APISocketPath, APIs: []string{"metrics"}, Mode: 0o666},
		{Name: "control", Address: "unix:" + monitoringapi.ControlSocketPath, APIs: []string{"metrics", "commands"}, Mode: 0o600, RootOnly: true},
	}
	for _, listener := range cfg.Listeners {
		out = append(out, app.ListenerOptions{
			Name:    listener.Name,
			Address: app.GetAddress(listener.Address),
			APIs:    []string{"metrics"},
			Plugins: append([]string(nil), listener.Plugins...),
		})
	}
	return out
}

func runOptions(cfg config.Config, configPath, source string) app.ReloadOptions {
	return app.ReloadOptions{
		CollectorInterval:    cfg.Collector.Interval.Duration(),
		SmartRefreshInterval: cfg.Collector.SmartRefreshInterval.Duration(),
		DiskUsageCache:       cfg.Collector.DiskUsageCache.Duration(),
		HistoryRetention:     cfg.History.Retention.Duration(),
		History:              cfg.HistoryString(),
		HistorySet:           true,
		ConfigSource:         source,
		ConfigVersion:        cfg.Version,
	}
}

// Run loads the config, writes it when absent, and blocks until ctx ends.
func Run(ctx context.Context, configPath string) error {
	cfg, loaded, err := config.Load(configPath)
	if err != nil {
		return err
	}
	source := "loaded"
	if !loaded {
		source = "defaults"
		if created, saveErr := config.SaveIfMissing(configPath, cfg); saveErr != nil {
			slog.Warn("could not write default config", "path", configPath, "err", saveErr)
		} else if created {
			source = "created"
		}
	}

	a, err := app.New(ctx, DataDir)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	reload := runOptions(cfg, configPath, source)
	return a.StartContext(ctx, app.RunOptions{
		Listeners:            Listeners(cfg),
		CollectorInterval:    reload.CollectorInterval,
		SmartRefreshInterval: reload.SmartRefreshInterval,
		DiskUsageCache:       reload.DiskUsageCache,
		HistoryRetention:     reload.HistoryRetention,
		History:              reload.History,
		HistorySet:           true,
		ConfigPath:           configPath,
		ConfigSource:         source,
		ConfigVersion:        cfg.Version,
		CommandExecutor:      NewCommandExecutor(a, configPath),
		ReloadConfig: func() (app.ReloadOptions, error) {
			reloaded, _, err := config.Load(configPath)
			if err != nil {
				return app.ReloadOptions{}, err
			}
			return runOptions(reloaded, configPath, "loaded"), nil
		},
	})
}
```

- [ ] **Step 4: Thread `DiskUsageCache` through the app**

In `runtime.go` add `DiskUsageCache time.Duration` to both `RunOptions` and `ReloadOptions`; in `StartContext` after `setRuntimeConfig`, and in `applyReload`, call `a.fsManager.setDiskUsageCache(opts.DiskUsageCache)`. In `disk.go` delete the `DISK_USAGE_CACHE` block from `newFsManager` and add:

```go
func (m *fsManager) setDiskUsageCache(d time.Duration) {
	if d < 0 {
		d = 0
	}
	m.diskUsageCacheDuration = d
}
```

If `diskUsageCacheDuration` is read outside the app mutex, guard the setter with the same mutex the readers use.

- [ ] **Step 5: Adapt the command executor**

In `command.go`:

- rename the type to `commandExecutor` with fields `app *app.App` and `configPath string`; add `func NewCommandExecutor(a *app.App, configPath string) httpapi.CommandExecutor { return &commandExecutor{app: a, configPath: configPath} }`;
- replace the `uuid` import and `uuid.NewV7().String()` with `rand.Text()` from `crypto/rand`;
- `config.get` returns `cfg.View()`; `config.set` decodes:

```go
type configSetParams struct {
	CollectorInterval    *string            `json:"collector_interval"`
	SmartRefreshInterval *string            `json:"smart_refresh_interval"`
	DiskUsageCache       *string            `json:"disk_usage_cache"`
	HistoryRetention     *string            `json:"history_retention"`
	History              *string            `json:"history"`
	Listeners            *[]config.Listener `json:"listeners"`
}
```

and `applyConfigSetParams` sets `cfg.Collector.Interval`, `cfg.Collector.SmartRefreshInterval`, `cfg.Collector.DiskUsageCache`, `cfg.History.Retention`, `cfg.History.Plugins` (split the comma string, trim, drop empties) and `cfg.Listeners` (restart required), then `config.Validate`; delete the `LegacyCacheTTL` and `AllowRemoteCommands` handling;
- `handleConfigReload` calls `config.Load(e.configPath)`;
- `reloadRuntimeConfig` passes `DiskUsageCache: cfg.Collector.DiskUsageCache.Duration()`;
- `listenersRestartRequired` compares `cfg.Listeners` against `active` after skipping the two runtimes named `api` and `control`.

Update `command_test.go` for the new constructor, the `View` shape of `config.get`, the `disk_usage_cache` field, and the removal of `allow_remote_commands` and `cache_ttl` cases. Keep the existing test cases for every other command.

- [ ] **Step 6: Write the CLI and entry point**

`backend/monitoring/internal/cli/main.go`:

```go
// Package cli parses the linuxio-monitoring command line.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/common/debugserver"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/daemon"
)

const usageText = `linuxio-monitoring is managed by LinuxIO and systemd.

Usage:
  linuxio-monitoring run [--config PATH] [--verbose]
  linuxio-monitoring --version
  linuxio-monitoring --help

Status, configuration, history and database maintenance are available through
the LinuxIO interface.
`

func Main(args []string) int {
	if len(args) == 0 {
		return writeOutput(os.Stdout, usageText)
	}
	switch args[0] {
	case "--version", "-v", "version":
		return writeOutput(os.Stdout, "LinuxIO Monitoring "+version.Version+"\n")
	case "--help", "-h", "help":
		return writeOutput(os.Stdout, usageText)
	case "run":
		return runDaemon(args[1:])
	}
	return writeError(os.Stderr, fmt.Sprintf("linuxio-monitoring: unknown command %q\n\n%s", args[0], usageText))
}

func runDaemon(args []string) int {
	fs := flag.NewFlagSet("linuxio-monitoring run", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath, "YAML config file path")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		return writeError(fs.Output(), fmt.Sprintf("linuxio-monitoring run does not accept arguments: %s\n", fs.Arg(0)))
	}
	if err := logging.Configure("linuxio-monitoring", *verbose); err != nil {
		return writeError(os.Stderr, fmt.Sprintf("linuxio-monitoring: initialize logging: %v\n", err))
	}
	debugserver.Start("127.0.0.1:6062")
	slog.Info("monitoring starting", "version", version.Version, "config", *configPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx, *configPath); err != nil {
		slog.Error("daemon exited with error", "err", err)
		return 1
	}
	slog.Info("shutdown complete")
	return 0
}

func writeOutput(w io.Writer, value string) int {
	if _, err := io.WriteString(w, value); err != nil {
		return 1
	}
	return 0
}

func writeError(w io.Writer, value string) int {
	_ = writeOutput(w, value)
	return 1
}
```

`backend/monitoring/main.go`:

```go
package main

import (
	"os"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
```

`cli/main_test.go`:

```go
package cli

import "testing"

func TestMainRejectsUnknownCommand(t *testing.T) {
	if code := Main([]string{"menu"}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestMainPrintsVersion(t *testing.T) {
	if code := Main([]string{"--version"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}
```

- [ ] **Step 7: Write the unit and the shipped default config**

`packaging/systemd/linuxio-monitoring.service`:

```ini
[Unit]
Description=LinuxIO Monitoring
Documentation=https://github.com/mordilloSan/LinuxIO
PartOf=linuxio.target
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# Root: SMART, the Docker socket, hwmon and GPU collectors need host devices.
User=root
Group=root
ExecStart=/usr/local/bin/linuxio-monitoring run --config /etc/linuxio/monitoring/config.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s
StateDirectory=linuxio/monitoring
StateDirectoryMode=0750
ConfigurationDirectory=linuxio/monitoring
RuntimeDirectory=linuxio/monitoring
RuntimeDirectoryMode=0755

# Host metrics, SMART and GPU collectors need real /proc, /sys, sockets and
# devices, so the hardening avoids private devices, network and proc.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/linuxio/monitoring /etc/linuxio/monitoring
ProtectHome=read-only
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectClock=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
SystemCallArchitectures=native

[Install]
WantedBy=linuxio.target
```

`packaging/etc/linuxio/monitoring/config.yaml`:

```yaml
version: 1
collector:
  interval: 1m0s
  smart_refresh_interval: 1h0m0s
  disk_usage_cache: 0s
history:
  retention: 720h0m0s
  plugins: [cpu, mem, swap, diskio, network, containers, container_telemetry]
listeners: []
```

- [ ] **Step 8: Run every monitoring package**

```bash
make test-go GO_TEST_PKGS=./monitoring/...
```

Expected: PASS.

- [ ] **Step 9: Stage**

```bash
git add backend/monitoring packaging/systemd/linuxio-monitoring.service packaging/etc/linuxio/monitoring/config.yaml
```

---

### Task 10: Build, release, install scripts, CLI, man page

**Files:**
- Modify: `Makefile`, `.github/workflows/release.yml`, `packaging/scripts/localinstall.sh`, `packaging/scripts/install-linuxio-binaries.sh`, `packaging/scripts/uninstall.sh`, `backend/cli/main.go`, `backend/cli/main_test.go` (if it covers `journalTermsForMode`), `packaging/man/linuxio.8`

**Interfaces:**
- Produces: `make build-monitoring` writes `linuxio-monitoring` to the repo root; `_build-binaries` includes it; `linuxio logs monitoring` and `linuxio version` know the binary.

- [ ] **Step 1: Makefile**

Add after `indexer_binary := $(bin_dir)/linuxio-indexer`:

```make
monitoring_binary := $(bin_dir)/linuxio-monitoring
```

Change `go_binary_targets := build-bridge build-cli build-docker-update build-indexer` to include `build-monitoring`, and add:

```make
build-monitoring: go_binary_label := monitoring daemon
build-monitoring: go_binary_package := ./monitoring
build-monitoring: go_binary_output := $(monitoring_binary)
build-monitoring: go_binary_ldflags := -s -w -X '$(MODULE_PATH)/common/version.Version=$(GIT_VERSION)' -X '$(MODULE_PATH)/common/version.CommitSHA=$(GIT_COMMIT_SHORT)' -X '$(MODULE_PATH)/common/version.BuildTime=$(BUILD_TIME)'
build-monitoring: go_binary_extra_env := CGO_ENABLED=1
build-monitoring: go_binary_tags := $(if $(filter amd64,$(GOARCH_HOST)),glibc,)
```

If the Makefile has no `GOARCH_HOST` variable, define `GOARCH_HOST ?= $(shell go env GOARCH 2>/dev/null || echo amd64)` next to `GOAMD64` and reuse it; NVML is compiled only on amd64 and the stub file covers other architectures. Add `@$(MAKE) --no-print-directory build-monitoring SKIP_ENSURE_GO=1` to `_build-binaries` after the indexer line, `"$(monitoring_binary)"` to the `clean` `rm -f` list, `build-monitoring` to the `.PHONY` line that lists `build-indexer`, and a help line next to the indexer's:

```make
	@$(PRINTC) "$(COLOR_YELLOW)    make build-monitoring $(COLOR_RESET) Build the monitoring daemon"
```

- [ ] **Step 2: Release workflow**

In `.github/workflows/release.yml` add after the indexer build step:

```yaml
      - name: Build monitoring daemon
        env: { GOOS: linux, GOARCH: amd64 }
        run: make build-monitoring SKIP_ENSURE_GO=1
```

Append `linuxio-monitoring` to the `chmod +x` line, the `tar czvf` list, the `sha256sum` list, the `artifacts:` list, and add to the verification step:

```bash
          chmod +x linuxio-monitoring
          ./linuxio-monitoring --version || { echo "Monitoring daemon verification failed"; exit 1; }
          echo "✓ linuxio-monitoring runs correctly"
```

Run `actionlint .github/workflows/release.yml`.

- [ ] **Step 3: Install scripts**

`localinstall.sh`: add `linuxio-monitoring` to `linuxio_binary_names`, `linuxio-monitoring.service` to `linuxio_systemd_units`, extend the config-preservation condition so `monitoring/config.yaml` is also not overwritten when present:

```bash
			if [[ ("$rel_path" == "indexer/config.yaml" || "$rel_path" == "monitoring/config.yaml") &&
				-f "/etc/linuxio/$rel_path" ]]; then
```

and update the two summary lines to list the binary and `/etc/linuxio/monitoring/config.yaml`. The service is pulled in by `linuxio.target` through `WantedBy`, and `linuxio restart --full` is not run by the installer, so add after `systemctl enable linuxio.target`:

```bash
	systemctl enable linuxio-monitoring.service >/dev/null 2>&1
	systemctl restart linuxio-monitoring.service >/dev/null 2>&1 || true
```

`install-linuxio-binaries.sh`: add the binary to `release_binary_names`, the unit to `release_systemd_units`, the same config-preservation rule wherever it installs `/etc/linuxio` files, a `--version` check next to the indexer's, and the enable/restart lines next to where it enables the indexer timer. `uninstall.sh`: add `linuxio-monitoring` to the `rm -f` binary line and stop/disable/remove `linuxio-monitoring.service` where the indexer units are handled; leave `/var/lib/linuxio/monitoring` and `/etc/linuxio/monitoring` alone unless the script already purges the indexer's state, in which case mirror it.

```bash
shellcheck packaging/scripts/localinstall.sh packaging/scripts/install-linuxio-binaries.sh packaging/scripts/uninstall.sh && shfmt -d packaging/scripts/localinstall.sh packaging/scripts/install-linuxio-binaries.sh packaging/scripts/uninstall.sh
make test-installation-scripts-quiet
```

Expected: clean, PASS.

- [ ] **Step 4: CLI**

In `backend/cli/main.go`: help text `logs        Tail logs [webserver|bridge|auth|indexer|monitoring] [lines] (default: all, 100)`; in `showVersion` add a block after the indexer's:

```go
	out, err = versionExecCommand("linuxio-monitoring", "--version").CombinedOutput()
	if err == nil {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %s\n", line)
	} else {
		fmt.Println("  linuxio-monitoring: not found or error")
	}
```

In the logs argument switch add `case "monitoring", "monitor": mode = "monitoring"`; in `journalTermsForMode` add `"SYSLOG_IDENTIFIER=linuxio-monitoring"` and `"_SYSTEMD_UNIT=linuxio-monitoring.service"` to the default list and:

```go
	case "monitoring":
		journalTerms = []string{
			"SYSLOG_IDENTIFIER=linuxio-monitoring",
			"_SYSTEMD_UNIT=linuxio-monitoring.service",
		}
```

If `backend/cli/main_test.go` asserts the exact default term list, extend the expectation.

- [ ] **Step 5: Man page**

In `packaging/man/linuxio.8` add `monitoring` to the `logs` component list and a unit entry after the indexer units:

```
.TP
.B linuxio-monitoring.service
Runs the monitoring daemon that samples host metrics and keeps their history.
```

- [ ] **Step 6: Build and run the CLI tests**

```bash
make build-monitoring && ./linuxio-monitoring --version
make test-go GO_TEST_PKGS=./cli/...
```

Expected: the binary prints `LinuxIO Monitoring <version>`; CLI tests PASS.

- [ ] **Step 7: Stage**

```bash
git add Makefile .github/workflows/release.yml packaging/scripts/localinstall.sh packaging/scripts/install-linuxio-binaries.sh packaging/scripts/uninstall.sh backend/cli packaging/man/linuxio.8
```

---

### Task 11: Repoint the bridge

**Files:**
- Modify: `backend/bridge/apischema/models.go`, `backend/bridge/handlers/monitoring/client.go`, `history.go`, `history_test.go`, `container_current.go`, `container_current_test.go`, `config.go`, `config_test.go`, `restart.go`, `status.go`, `backend/bridge/handlers/system/capabilities.go`, `capabilities_test.go`, `backend/bridge/handlers/packages/install_capability.go`
- Create: `backend/bridge/handlers/monitoring/live.go`, `live_test.go`
- Delete: `backend/bridge/handlers/packages/install_monitoring.go`, `install_monitoring_test.go`
- Regenerate: `frontend/src/api/generated/*` via `make generate`

**Interfaces:**
- Produces:
  - `monitoring.FetchLive(ctx context.Context) (monitoringapi.Live, error)` over `api.sock`; `monitoring.ErrUnavailable`.
  - `monitoring.FetchContainerMetricsSnapshot(ctx) (ContainerMetricsSnapshot, error)` built from `FetchLive`, same result type as today.
  - apischema: `MonitoringListener{Name, Address string; Plugins []string}`; `MonitoringConfig{Version int; CollectorInterval, SmartRefreshInterval, DiskUsageCache, HistoryRetention, History string; Listeners []MonitoringListener}`; `MonitoringConfigPatch` with pointer fields for the same minus `Version`, `Listeners []MonitoringListener`.
  - Capability `monitoring` detection: `GET /healthz` on `api.sock` returns 200.

- [ ] **Step 1: Update apischema**

In `models.go` replace `MonitoringListener`, `MonitoringConfig`, `MonitoringConfigPatch`:

```go
type MonitoringListener struct {
	Address string   `json:"address"`
	Name    string   `json:"name"`
	Plugins []string `json:"plugins,omitempty"`
}

type MonitoringConfig struct {
	CollectorInterval    string               `json:"collector_interval"`
	DiskUsageCache       string               `json:"disk_usage_cache"`
	History              string               `json:"history"`
	HistoryRetention     string               `json:"history_retention"`
	Listeners            []MonitoringListener `json:"listeners"`
	SmartRefreshInterval string               `json:"smart_refresh_interval"`
	Version              int                  `json:"version"`
}

type MonitoringConfigPatch struct {
	CollectorInterval    *string              `json:"collector_interval,omitempty"`
	DiskUsageCache       *string              `json:"disk_usage_cache,omitempty"`
	History              *string              `json:"history,omitempty"`
	HistoryRetention     *string              `json:"history_retention,omitempty"`
	Listeners            []MonitoringListener `json:"listeners,omitempty"`
	SmartRefreshInterval *string              `json:"smart_refresh_interval,omitempty"`
}
```

`MonitoringStatus` and `MonitoringListenerStatus` stay as they are.

- [ ] **Step 2: Two fixed clients**

Rewrite the top of `client.go`:

```go
const maxCommandPayloadBytes = 1 << 20

var (
	commandRetryInterval = 150 * time.Millisecond
	commandRetryTimeout  = 5 * time.Second
)

// ErrUnavailable marks a daemon that cannot be reached.
var ErrUnavailable = errors.New("linuxio-monitoring unavailable")

func unixClient(socketPath string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

// controlClient reaches control.sock: commands and privileged history reads.
// apiClient reaches api.sock: live reads any session may perform.
var (
	controlClient = unixClient(monitoringapi.ControlSocketPath, 0)
	apiClient     = unixClient(monitoringapi.APISocketPath, 15*time.Second)
)
```

Replace `monitoringClient.Do(req)` with `controlClient.Do(req)` in `doCommandRequest`. Import `monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"`.

- [ ] **Step 3: History over the control socket**

In `history.go` delete `newMetricsClient`, `resolveMetricsListenerFromStatus` and every use of `FetchStatus` for address resolution. `fetchHistory` builds `http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/api/v1/"+plugin+"/history?"+query.Encode(), nil)` and sends it with `controlClient`. Update `history_test.go`: replace `withTestMetricsClient` with a `withTestControlClient` helper that swaps `controlClient`'s transport for a `roundTripFunc`, drop the `status.get` expectations, and keep every payload assertion.

- [ ] **Step 4: Write the failing live client test**

`live_test.go`:

```go
package monitoring

import (
	"context"
	"errors"
	"net/http"
	"syscall"
	"testing"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

func withTestAPIClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := apiClient
	apiClient = &http.Client{Transport: fn}
	t.Cleanup(func() { apiClient = orig })
}

func TestFetchLiveDecodesPayload(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != monitoringapi.RouteLive {
			t.Fatalf("path = %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"captured_at_ms": 1700000000000, "cpu": {"percent": 3.5, "per_core_percent": [1, 6]}, "memory": {"total_bytes": 10}, "containers": {"captured_at_ms": 1700000000000, "items": []}}`), nil
	})
	live, err := FetchLive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if live.CapturedAtMs != 1700000000000 || live.CPU.PerCorePercent[1] != 6 || live.Memory.TotalBytes != 10 {
		t.Fatalf("live = %+v", live)
	}
}

func TestFetchLiveMapsDialFailureToUnavailable(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) { return nil, syscall.ECONNREFUSED })
	if _, err := FetchLive(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
```

- [ ] **Step 5: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./bridge/handlers/monitoring/... GO_TEST_FLAGS='-run TestFetchLive'
```

Expected: FAIL to compile.

- [ ] **Step 6: Implement `live.go`**

```go
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const maxLivePayloadBytes = 4 << 20

// FetchLive reads the daemon's live payload over api.sock. Any session may
// call it; the socket is world-readable like /proc.
func FetchLive(ctx context.Context) (monitoringapi.Live, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+monitoringapi.RouteLive, nil)
	if err != nil {
		return monitoringapi.Live{}, fmt.Errorf("create live request: %w", err)
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return monitoringapi.Live{}, ctx.Err()
		}
		return monitoringapi.Live{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return monitoringapi.Live{}, fmt.Errorf("%w: live returned %s", ErrUnavailable, resp.Status)
	}
	var live monitoringapi.Live
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxLivePayloadBytes)).Decode(&live); err != nil {
		return monitoringapi.Live{}, fmt.Errorf("decode live payload: %w", err)
	}
	return live, nil
}
```

- [ ] **Step 7: Container metrics from live**

Rewrite `FetchContainerMetricsSnapshot` in `container_current.go`:

```go
func FetchContainerMetricsSnapshot(ctx context.Context) (ContainerMetricsSnapshot, error) {
	live, err := FetchLive(ctx)
	if err != nil {
		return ContainerMetricsSnapshot{}, err
	}
	samples := make(map[string]ContainerMetricSample, len(live.Containers.Items))
	for index, item := range live.Containers.Items {
		if item.ID == "" {
			return ContainerMetricsSnapshot{}, fmt.Errorf("validate container metrics item %d: empty id", index)
		}
		if _, exists := samples[item.ID]; exists {
			return ContainerMetricsSnapshot{}, fmt.Errorf("validate container metrics item %d: duplicate container ID %q", index, item.ID)
		}
		if item.CPUPercent < 0 || math.IsNaN(item.CPUPercent) || math.IsInf(item.CPUPercent, 0) {
			return ContainerMetricsSnapshot{}, fmt.Errorf("validate container metrics item %d: invalid cpu percent", index)
		}
		samples[item.ID] = ContainerMetricSample{
			ID:                           item.ID,
			CPUPercent:                   item.CPUPercent,
			MemoryUsageBytes:             item.MemoryBytes,
			NetworkSendBytesPerSecond:    item.TxBytesPerSec,
			NetworkReceiveBytesPerSecond: item.RxBytesPerSec,
			BlockReadBytesPerSecond:      item.BlockReadBytesPerSec,
			BlockWriteBytesPerSecond:     item.BlockWriteBytesPerSec,
		}
	}
	return ContainerMetricsSnapshot{
		CapturedAtMs:      live.Containers.CapturedAtMs,
		CollectorInterval: defaultContainerCollectorInterval,
		Samples:           samples,
	}, nil
}
```

Delete the HTTP fetch helpers, the telemetry merge, `containerCollectorInterval`, `withinCollectorInterval` and the `logicalCPUCount` use in this file (history.go still uses `logicalCPUCount` for history conversion). `CollectorInterval` stays in the struct because `docker/container.go` derives its staleness window from it; with a 5-second default and live data the window becomes one minute, which matches today. Rewrite `container_current_test.go` to feed `withTestAPIClient` with a live payload and assert the mapped samples, the duplicate-id rejection, the telemetry-nil case, and cancellation.

- [ ] **Step 8: Config, restart, status**

`config.go`: `patchIsEmpty` checks the new pointer set (`CollectorInterval`, `SmartRefreshInterval`, `DiskUsageCache`, `History`, `HistoryRetention`, `len(Listeners) == 0`). `restart.go`: `const monitoringServiceName = "linuxio-monitoring.service"`. `status.go` unchanged. Update `config_test.go` payload expectations: remove `allow_remote_commands`, add `disk_usage_cache`, listeners carry `plugins` instead of `apis`.

- [ ] **Step 9: Capability detection and install removal**

In `capabilities.go` replace `checkMonitoringAvailability`, `monitoringCLILookPath` and `monitoringCLIOutput` with:

```go
var monitoringHealthClient = &http.Client{
	Timeout: monitoringHealthTimeout,
	Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", monitoringapi.APISocketPath)
	}},
}

func checkMonitoringAvailability(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/healthz", nil)
	if err != nil {
		return false, err
	}
	resp, err := monitoringHealthClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("linuxio-monitoring is not running: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("linuxio-monitoring health %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return true, nil
}
```

Change the registry entry to `LogName: "linuxio-monitoring"` and delete its `Install` block and the `OptionalComponentMonitoring` constant. In `install_capability.go` delete `installOptionalComponent`'s monitoring case (if the function then has no cases, delete the function and its call site, keeping the `unknown optional component` error where the switch was). Delete `install_monitoring.go` and `install_monitoring_test.go`. In `capabilities_test.go` replace the three monitoring tests with one that swaps `monitoringHealthClient`'s transport for a `roundTripFunc` returning 200 and one returning 503 with a body, asserting `ok` and the error message respectively.

- [ ] **Step 10: Generate and test**

```bash
make generate
make test-go GO_TEST_PKGS='./bridge/... ./common/...'
```

Expected: PASS; `frontend/src/api/generated/linuxio-types.ts` shows `MonitoringConfig` with `disk_usage_cache` and without `allow_remote_commands`.

- [ ] **Step 11: Stage**

```bash
git add backend/bridge frontend/src/api/generated
git rm -q backend/bridge/handlers/packages/install_monitoring.go backend/bridge/handlers/packages/install_monitoring_test.go
```

---

### Task 12: Frontend capability entry and settings section

**Files:**
- Modify: `frontend/src/api/capabilities.ts`, `frontend/src/api/capabilities.test.ts`, `frontend/src/routes/_authenticated/-components/navbar/MonitoringSettingsSection.tsx`, its test file if present, `frontend/src/constants/liveCharts.ts`, `frontend/src/components/charts/useLiveSeries.ts`, `frontend/src/components/charts/liveSeriesStore.ts`

- [ ] **Step 1: Update the capability test first**

In `capabilities.test.ts` change the monitoring test to:

```ts
  it("defines monitoring as a built-in component", () => {
    const monitoring = CAPABILITIES.find(
      (capability) => capability.wire === "monitoring",
    );
    expect(monitoring).toMatchObject({
      dependency: "linuxio-monitoring",
      state: "monitoringAvailable",
    });
    expect(monitoring?.installable).toBeUndefined();
  });
```

Run `make test-frontend-quiet` scoped if the Makefile supports a pattern, otherwise run it whole after Step 3.

- [ ] **Step 2: Update the entry**

```ts
  {
    wire: "monitoring",
    state: "monitoringAvailable",
    label: "Monitoring",
    description: "Host metrics daemon: live sampling and history",
    readyText: "linuxio-monitoring is healthy.",
    dependency: "linuxio-monitoring",
    icon: "mdi:chart-line",
    reasonUnknown: "Monitoring availability is still being checked.",
    reasonUnavailable: "linuxio-monitoring is not running.",
  },
```

- [ ] **Step 3: Settings section**

In `MonitoringSettingsSection.tsx`:

- `DraftConfig`: remove `allow_remote_commands`; add `disk_usage_cache: string`; `listeners: MonitoringListener[]` now carries `plugins?: string[]`.
- `toDraft`: `disk_usage_cache: compactGoDuration(config.disk_usage_cache)`, listeners map `{ ...listener, plugins: [...(listener.plugins ?? [])] }`.
- `normalizeListeners`: trim `name` and `address`, keep `plugins` filtered to non-empty trimmed strings; drop the `apis` handling.
- `toPatchPayload`: emit `disk_usage_cache` when changed; delete the `allow_remote_commands` branch.
- `validateDraft`: `disk_usage_cache` must be a Go duration; zero disables the cache, so only `isGoDuration` is checked; listener name required; the address validator stays.
- Fields: add a duration `AppTextField` for "Disk usage cache" next to the SMART refresh field with helper text "How often filesystem usage is re-read; keeps sleeping disks asleep."; remove the remote-commands `ToggleCard`.
- Listener editor: each row shows name, address, and a plugin multi-select built from the plugin names returned by `linuxio.monitoring.get_status`'s `config.history_plugins` union the fixed list `["cpu","mem","swap","load","diskio","fs","network","gpu","sensors","containers","container_telemetry","processes","programs","connections","irq","smart"]`; an empty selection means all plugins. Add an `AppAlert severity="info"` above the listener rows: "Listeners are unauthenticated. Anyone who can reach the address can read the selected metrics."
- Toasts: "Restart linuxio-monitoring to apply listener changes." and "linuxio-monitoring restarted"; error strings likewise.
- Status card: keep listeners; the two fixed sockets now appear as `api` and `control`.

Update the section's tests for the removed toggle and the new field. Fix the comments in `liveCharts.ts` (`/** How far back to seed from monitoring history (collector interval samples). */`), `useLiveSeries.ts` and `liveSeriesStore.ts` (`go-monitoring` to `linuxio-monitoring`).

- [ ] **Step 4: Run the frontend checks**

```bash
make check-frontend-quiet
```

Expected: PASS. Inspect `.cache/test-logs/` on failure.

- [ ] **Step 5: Stage**

```bash
git add frontend/src/api/capabilities.ts frontend/src/api/capabilities.test.ts frontend/src/routes/_authenticated/-components/navbar frontend/src/constants/liveCharts.ts frontend/src/components/charts
```

---

### Task 13: Docs, notices, lint pass, full verification

**Files:**
- Create: `docs/monitoring.md`
- Modify: `docs/THIRD_PARTY_NOTICES.md`, `docs/process-systemd-architecture.md`, `docs/capabilities.md`, `README.md`, `docs/TODO/linuxio-monitoring.md` (status line)

- [ ] **Step 1: Third-party notices**

Add after the FileBrowser section of `docs/THIRD_PARTY_NOTICES.md`:

```markdown
## Beszel

Portions of `linuxio-monitoring` derive from
[Beszel](https://github.com/henrygd/beszel) by henrygd, by way of the
go-monitoring fork at https://github.com/mordilloSan/go-monitoring, whose
history before the import into LinuxIO is kept there.

MIT License

Copyright (c) 2024 henrygd

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

Add sections for `purego` (Apache-2.0, ebitengine) if the file lists every direct dependency; follow the existing pattern.

- [ ] **Step 2: Service guide**

Write `docs/monitoring.md` with these sections, in the shape of `docs/indexer.md`: purpose; trust boundary diagram (browser to webserver to bridge to `api.sock`/`control.sock`); installed files and units table; sampling semantics (collector tick, live reuse, stale reseed); HTTP API summary with `/api/v1/live` and the plugin allowlist rule; config file with the YAML example from the spec; capability and failure behaviour; troubleshooting (`linuxio logs monitoring`, `curl --unix-socket /run/linuxio/monitoring/api.sock http://localhost/healthz`).

- [ ] **Step 3: Architecture and capability docs**

`docs/process-systemd-architecture.md`: add `linuxio-monitoring` rows to the "Why Multiple Binaries", "Binaries" and "Binary invocation map" tables, a `linuxio-monitoring.service` line to the unit tree, a `make build-monitoring` row to the build table, and `monitoring` to the CLI `logs` list; count of release artifacts becomes seven. `docs/capabilities.md`: replace the go-monitoring optional-component sentence with "The monitoring daemon ships with LinuxIO and is not installed through Capability Manager; its capability reports daemon health." `README.md`: mention `linuxio-monitoring` in the binaries list of the install section. Set the spec's status line to "Plan 1 implemented; Plans 2 to 4 pending".

- [ ] **Step 4: Lint the copied tree**

```bash
make golint-only
```

Expected: findings only in `backend/monitoring`. Fix them in place: wrap returned errors where `wrapcheck` asks, add `ctx` where `noctx` asks, and use targeted `//nolint:<linter> // reason` only where the copied design needs it (for example the `gocognit` markers go-monitoring already carries). Re-run until clean.

- [ ] **Step 5: Full verification**

```bash
make check-backend-quiet
make check-frontend-quiet
make test-quiet
```

Expected: all PASS. Report the exact targets and outcomes.

- [ ] **Step 6: Runtime verification on the development host**

```bash
sudo make localinstall
systemctl status linuxio-monitoring.service --no-pager
ls -l /run/linuxio/monitoring/
curl -s --unix-socket /run/linuxio/monitoring/api.sock http://localhost/healthz
curl -s --unix-socket /run/linuxio/monitoring/api.sock http://localhost/api/v1/live | head -c 600
sudo curl -s --unix-socket /run/linuxio/monitoring/control.sock -H 'Content-Type: application/json' -d '{"command":"status.get"}' http://localhost/api/v1/command | head -c 400
curl -s --unix-socket /run/linuxio/monitoring/control.sock http://localhost/healthz; echo
linuxio logs monitoring 20
```

Expected: service active; `api.sock` mode `srw-rw-rw-`, `control.sock` mode `srw-------`; `/healthz` returns 503 for about one collector interval after start and then 200; the live payload has `captured_at_ms`; the root command returns `"ok":true`; the unprivileged `control.sock` call is refused. In the browser: hardware history cards, network traffic history, Docker container metrics, and the monitoring settings section (save a shorter collector interval, add a TCP listener with `plugins: [cpu]`, restart, `curl http://127.0.0.1:<port>/api/v1/live` shows only cpu).

- [ ] **Step 7: Stage**

```bash
git add docs/monitoring.md docs/THIRD_PARTY_NOTICES.md docs/process-systemd-architecture.md docs/capabilities.md README.md docs/TODO/linuxio-monitoring.md backend/monitoring
```

Suggested commit message for the user:

```
feat(monitoring): import go-monitoring as linuxio-monitoring

Ship the metrics daemon as a first-party binary with fixed unix sockets,
strict YAML config, optional read-only listeners with plugin allowlists,
a byte-precise /api/v1/live route, one-second live sample reuse, and
stale-baseline reseeding from the collector. Repoint the bridge's
monitoring routes and container metrics to it and retire the external
capability install path.
```

---

## Self-Review Notes

- Spec coverage: process model (Task 9, 10), sockets and peer gate (Task 5, 9), config (Task 6), sampling semantics (Task 7), HTTP API and allowlists (Task 5, 8), `api.Live` first sections (Task 8), bridge repoint and capability (Task 11), settings and capability UI (Task 12), trims and dependencies (Task 1, 2, 3, 4, 9), build/release/install/CLI/man (Task 10), docs and notices (Task 13), tests throughout. Bridge live-route migration, moved collectors, the process page and the moby swap are Plans 2 to 4.
- Type names used across tasks: `app.ListenerOptions{Mode, RootOnly, Plugins}` (Task 5, 9), `httpapi.Options{LastCollected, Live}` (Task 4, 8), `config.Config/View/Listener` (Task 6, 9, 11), `monitoringapi.Live` and socket constants (Task 6, 8, 11), `monitoring.FetchLive/ErrUnavailable` (Task 11).
