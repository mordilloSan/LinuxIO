# Bridge Handler Patterns (ADR)

**Status:** Proposed
**Date:** 2026-05-10
**Companion docs:** [bridge-handler-api.md](./bridge-handler-api.md), [PRIVILEGE_PATTERN.md](./PRIVILEGE_PATTERN.md)

This ADR locks in the **per-package handler-body conventions** for bridge IPC and stream handlers. It is complementary to `bridge-handler-api.md` (which documents the immutable infrastructure: frame protocol, registry, `Events`, `Handler`). Nothing in this ADR changes the IPC layer or wire protocol.

---

## 1. Context

`bridge-handler-api.md` defines what the bridge **is**. It does not define how a handler package **writes its handlers**. Without that convention, every package picked its own approach. After 16 handler packages, four mutually incompatible styles coexist:

| Style | Packages | Body shape |
|---|---|---|
| **Clean free fns + tiny helpers** | `accounts`, `indexer`, `power`, `jobs`, `control` | `handleXxx`, optional `requireArgs` / `decodeJSON` / `emitResult` helpers |
| **Verbose inline** | `storage`, `shares` | `handleXxx` with 4–5 lines of `slog.Debug/Info/Error` per error path |
| **Adapter pile** | `docker` (8 adapters + 12 factories + 10 log fns), `dbus` (4 adapters + 12 factories), `filebrowser` (2 adapters), `system` (3 adapters) | Generic per-shape adapters: `dockerNoArgCall`, `dbusNoArgResultHandler`, `emitFilebrowserArgsResult`, `emitSystemCall`, etc. |
| **Closure factories** | `config`, `docker` | `handleXxx(username, store) ipc.HandlerFunc` returning a closure |

Cost of inaction: each new handler picks the closest local style; drift compounds; cleanups become per-package crusades. The docker package is already the worst case (8 + 12 + 10 = 30 adapter-shaped artifacts for 42 commands).

**Compatibility wrappers caused most of this mess.** When a new style was introduced (e.g. `*WithStore` functions, `dockerHandlers` struct attempts, `system` privilege flag), the old style was kept "just for compatibility." It then never died. This ADR forbids that.

---

## 2. Decision Summary

1. Move `handlers/config` domain code to `bridge/settings/`. `handlers/config/` keeps only IPC handlers.
2. Add `bridge/runtime.Runtime` carrying `Session` and `Store`. Threaded through every `RegisterHandlers` call.
3. Add `handlers/internal/rpc` with one type + five functions. No business-shape adapters.
4. Privilege becomes registration metadata: `rpc.Command{Privileged: true}`. The existing `privilege.RequirePrivilegedIPC` is reused unchanged.
5. Stream-handler registry signature changes to take `Runtime`.
6. Seven official handler patterns documented below.
7. Packages migrate atomically, one at a time, in a defined order.
8. **No long-lived compatibility wrappers.** Each migration deletes the old shape in the same change that introduces the new one.

---

## 3. The Helper Package

`backend/bridge/handlers/internal/rpc`. Total public surface: 1 type + 5 functions. Logging is **not** abstracted — handlers write `slog.Info` inline.

```go
package rpc

import (
    "encoding/json"

    "github.com/mordilloSan/LinuxIO/backend/bridge/privilege"
    "github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
    "github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type Command struct {
    Name       string
    Handler    ipc.HandlerFunc
    Privileged bool
}

// Register installs commands into the IPC registry under the given component.
// Commands marked Privileged are wrapped by privilege.RequirePrivilegedIPC.
func Register(component string, rt runtime.Runtime, commands []Command) {
    for _, cmd := range commands {
        h := cmd.Handler
        if cmd.Privileged {
            h = privilege.RequirePrivilegedIPC(rt.Session, h)
        }
        ipc.RegisterFunc(component, cmd.Name, h)
    }
}

// Arg returns args[i] or ipc.ErrInvalidArgs if missing.
func Arg(args []string, i int) (string, error) {
    if len(args) <= i {
        return "", ipc.ErrInvalidArgs
    }
    return args[i], nil
}

// RequireArgs returns ipc.ErrInvalidArgs unless len(args) >= n.
func RequireArgs(args []string, n int) error {
    if len(args) < n {
        return ipc.ErrInvalidArgs
    }
    return nil
}

// DecodeJSONArg parses args[i] as JSON into T. Missing or malformed args
// return ipc.ErrInvalidArgs. The framework currently emits all returned
// errors as code 500 (see generic/bridge.go); finer-grained mapping is
// out of scope for this ADR.
func DecodeJSONArg[T any](args []string, i int) (T, error) {
    var zero T
    raw, err := Arg(args, i)
    if err != nil {
        return zero, err
    }
    var v T
    if err := json.Unmarshal([]byte(raw), &v); err != nil {
        return zero, ipc.ErrInvalidArgs
    }
    return v, nil
}

// EmitResult emits result on success, returns err on failure.
func EmitResult(emit ipc.Events, result any, err error) error {
    if err != nil {
        return err
    }
    return emit.Result(result)
}
```

**What is forbidden in this package:**

- Functions named `NoArgCall`, `OneArgCall`, `LoggedCall`, `UserCall`, etc.
- Wrappers that take a function and return a `HandlerFunc` based on the function's *shape*.
- Logging decorators of any kind.
- Generic dispatch by argument count.

If a future change tempts you to add one of these, the answer is: write the boilerplate inline. Three duplicate lines beats a 10-line adapter that has to be remembered.

---

## 4. The Runtime Type

`backend/bridge/runtime/runtime.go`:

```go
package runtime

import (
    "github.com/mordilloSan/LinuxIO/backend/bridge/settings"
    "github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Runtime carries process-wide handler dependencies.
// Always pass by value — it's two pointers.
//
// Invariant: after New(), Session and Store are non-nil. Accessor
// methods do not check nil; they will panic if called on a
// zero-value Runtime. RegisterAllHandlers is the single entry point
// that validates this — handler packages may assume the invariant.
type Runtime struct {
    Session *session.Session
    Store   *settings.UserStore
}

// New constructs a Runtime, panicking if either dependency is nil.
// Production code MUST go through New so the invariant is enforced
// at one site.
func New(sess *session.Session, store *settings.UserStore) Runtime {
    if sess == nil {
        panic("runtime: nil session")
    }
    if store == nil {
        panic("runtime: nil store")
    }
    return Runtime{Session: sess, Store: store}
}

func (r Runtime) Username() string { return r.Session.User.Username }
func (r Runtime) Privileged() bool { return r.Session.Privileged }
```

**Always passed**, even to packages that don't use both fields. The cost is one two-word struct copy at registration time; the benefit is that every `RegisterHandlers` signature in the bridge becomes identical:

```go
func RegisterHandlers(rt runtime.Runtime)
```

That uniformity is more valuable than saving a struct field somewhere.

Tests may construct `runtime.Runtime{Session: ..., Store: ...}` directly with non-nil fields. The struct literal is permitted; the validation in `New` is for production-path discipline.

---

## 5. The Seven Official Patterns

Every IPC handler in the bridge falls into one of these. The shape is non-negotiable; if a handler doesn't fit, that's a sign it's doing too much and needs to be split.

### 5.1 Stateless RPC

For packages whose handlers don't need `Session` or `Store` (`accounts`, `storage`, `shares`, `dbus`, `wireguard`, mostly).

```go
// backend/bridge/handlers/accounts/handlers.go

package accounts

import (
    "context"

    "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
    "github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
    "github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func RegisterHandlers(rt runtime.Runtime) {
    rpc.Register("accounts", rt, []rpc.Command{
        {Name: "list_users", Handler: handleListUsers},
        {Name: "delete_user", Handler: handleDeleteUser},
    })
}

func handleListUsers(ctx context.Context, args []string, emit ipc.Events) error {
    return rpc.EmitResult(emit, ListUsers())
}
```

`rt` is accepted but ignored when not needed. Handlers are free functions.

### 5.2 Stateful RPC

For packages where many handlers need `Runtime` (`docker`, `filebrowser`, `system`, `config`).

```go
// backend/bridge/handlers/docker/handlers.go

type dockerHandlers struct {
    rt runtime.Runtime
}

func RegisterHandlers(rt runtime.Runtime) {
    h := dockerHandlers{rt: rt}
    rpc.Register("docker", rt, []rpc.Command{
        {Name: "list_containers", Handler: h.listContainers},
        {Name: "start_container", Handler: h.startContainer},
        {Name: "compose_up",      Handler: h.composeUp},
    })
}

func (h dockerHandlers) startContainer(ctx context.Context, args []string, emit ipc.Events) error {
    id, err := rpc.Arg(args, 0)
    if err != nil {
        return err
    }
    slog.Info("start_container requested", "component", "docker", "container", id)
    return rpc.EmitResult(emit, StartContainer(ctx, id))
}
```

This is the replacement for closure factories like `handleStartContainer(username, store) ipc.HandlerFunc`. The struct is value-receiver; it carries two pointers.

### 5.3 JSON Request RPC

When the args slice is `[json_payload]`. Decoding lives at the boundary, not in an adapter.

```go
type setAutoUpdateRequest struct {
    Container string `json:"container"`
    Enabled   bool   `json:"enabled"`
}

func (h dockerHandlers) setAutoUpdate(ctx context.Context, args []string, emit ipc.Events) error {
    req, err := rpc.DecodeJSONArg[setAutoUpdateRequest](args, 0)
    if err != nil {
        return err
    }
    if req.Container == "" {
        return ipc.ErrInvalidArgs
    }
    slog.Info("set_auto_update requested", "component", "docker", "container", req.Container, "enabled", req.Enabled)
    return rpc.EmitResult(emit, doSetAutoUpdate(h.rt, req))
}
```

No `JSONHandler(fn)` adapter, ever.

### 5.4 Privileged RPC

Privilege is registration metadata. Handler bodies don't check `sess.Privileged`.

```go
rpc.Register("indexer", rt, []rpc.Command{
    {Name: "get_config", Handler: handleGetConfig, Privileged: true},
    {Name: "get_status", Handler: handleGetStatus, Privileged: true},
    {Name: "set_config", Handler: handleSetConfig, Privileged: true},
})
```

`rpc.Register` wraps with `privilege.RequirePrivilegedIPC(rt.Session, handler)` when `Privileged: true`. `system` already prototyped this pattern with a `privileged: true` field on its registration struct — this generalizes it.

### 5.5 Progress RPC

Same handler signature; the handler passes `emit` down to the operation.

```go
func (h fbHandlers) resourcePatch(ctx context.Context, args []string, emit ipc.Events) error {
    slog.Info("resource_patch requested", "component", "filebrowser")
    return rpc.EmitResult(emit, resourcePatchWithProgress(ctx, args, emit))
}
```

`emit.Progress(...)` calls happen inside the operation function. No special pattern needed.

### 5.6 Bidirectional RPC

Bidirectional RPC over the `bridge` stream is retired. Raw streams are the supported protocol for terminal-style I/O.

The old `terminal.bash` / `terminal.sh` bidirectional RPC commands were legacy. The frontend uses raw stream openers (`openTerminalStream` and `openContainerStream`), and a repository search found no current caller for `terminal.bash` or `terminal.sh`. Those registrations, the private `terminalHandler` implementation, the `generic.handleBidirectional` dispatcher branch, and the unused `ipc.BidirectionalHandler` interface have been removed.

The terminal package now has one normal RPC command:

```go
func RegisterHandlers(rt runtime.Runtime) {
    rpc.Register("terminal", rt, []rpc.Command{
        {Name: "list_shells", Handler: handleListShells},
    })
}
```

Host terminal I/O and container terminal I/O are both raw stream handlers, documented in 5.7. This avoids keeping two terminal protocols alive for the same frontend workflow.

### 5.7 Raw Stream

For yamux streams that aren't request/response. Confirmed currently-registered raw streams (per [`handlers/register.go`](../backend/bridge/handlers/register.go) and the four producer packages):

| Stream type | Producer | Registered at |
|---|---|---|
| `bridge` | `generic.HandleBridgeStream` | [register.go:43](../backend/bridge/handlers/register.go#L43) |
| `app-update` | `control` | [control/update_stream.go:36](../backend/bridge/handlers/control/update_stream.go) |
| `terminal` | `terminal` | [terminal/stream.go:39](../backend/bridge/handlers/terminal/stream.go) |
| `container` | `terminal` | same |
| `jobs-attach` | `jobs` | [jobs/handlers.go:30](../backend/bridge/handlers/jobs/handlers.go#L30) |
| `jobs-data` | `jobs` | same |
| `jobs-events` | `jobs` | same |
| `general-logs` | `logs` | [logs/handlers.go:11](../backend/bridge/handlers/logs/handlers.go#L11) |
| `service-logs` | `logs` | same |
| `docker-logs` | `logs` (handler in `docker`) | same |

Other names that appear in `bridge-handler-api.md` (`docker-compose`, `docker-indexer`, `fb-upload`, `fb-download`, `fb-archive`, `pkg-update`, etc.) are either job types dispatched through `jobs-*`, frontend transport-level concepts, or stale documentation. They are **not** currently registered raw streams. Reconciling that doc is out of scope here; this ADR governs only the table above.

**Signature change.** Map value goes from:

```go
func(*session.Session, net.Conn, []string) error
```

to:

```go
type StreamHandler func(runtime.Runtime, net.Conn, []string) error

var streamHandlers = map[string]StreamHandler{}
```

**Registration takes only the registry.** Per-package `RegisterStreamHandlers` does **not** receive `Runtime`. State is supplied at execution time by the dispatcher in [`bridge/main.go` `handleYamuxStream`](../backend/bridge/main.go), which already knows the `Runtime`:

```go
// In each producer package — registration is pure.
func RegisterStreamHandlers(streamHandlers map[string]StreamHandler) {
    streamHandlers["app-update"] = HandleAppUpdateStream
}

// HandleAppUpdateStream now receives Runtime at execution.
func HandleAppUpdateStream(rt runtime.Runtime, stream net.Conn, args []string) error {
    // ...
}
```

This eliminates the current asymmetry where `jobs.RegisterStreamHandlers(streamHandlers, deps.ConfigStore)` takes a store at registration time (and binds it via a closure for `jobs-events`). After this change, `jobs` reads `rt.Store` at execution time like everyone else; the special-case parameter goes away.

---

## 6. The Settings Move

`handlers/config/` mixes domain logic (Settings struct, validation, persistence) with IPC handlers (`handlers.go`). Moving the domain out of the handlers tree fixes the import cycle problem: today, `docker`, `filebrowser`, `jobs`, `system` all import `handlers/config` to get `*config.UserStore`. They should import a domain package, not a sibling handler package.

### 6.1 Destination

`backend/bridge/settings/`. Reasons:

- "Settings" is the user-facing word in the frontend (`AppSettings`, `Settings` page).
- "Userstore" is too narrow (the package owns more than the store).
- "State" is too generic.

### 6.2 What moves

| File | Lines | Disposition |
|---|---|---|
| `colors.go` | 70 | → `bridge/settings/colors.go` |
| `init.go` | 76 | → `bridge/settings/init.go` |
| `settings.go` | 154 | → `bridge/settings/settings.go` |
| `settings_test.go` | 84 | → `bridge/settings/settings_test.go` |
| `store.go` | 229 | → `bridge/settings/store.go` |
| `store_clone.go` | 109 | → `bridge/settings/store_clone.go` |
| `store_test.go` | 107 | → `bridge/settings/store_test.go` |
| `types.go` | 185 | → `bridge/settings/types.go` |
| `utils.go` | 203 | → `bridge/settings/utils.go` |
| `validator.go` | 369 | → `bridge/settings/validator.go` |
| `generator.go` | 53 | → `bridge/settings/generator.go` |
| `config_generated.yaml` | — | → `bridge/settings/config_generated.yaml` |
| `handlers.go` | 454 | **stays** in `handlers/config/handlers.go` |

The 454-line `handlers.go` contains the IPC payload types (`configSetPayload`, `configThemeColorsPayload`, etc.) — those are IPC-contract types, not domain. They stay in the handler package.

### 6.3 Type renames

`config.X` → `settings.X` for: `Settings`, `UserStore`, `AppSettings`, `Theme`, `ThemeLight`, `ThemeDark`, `ThemeColors`, `ThemeColorsByMode`, `Docker`, `DockerProxy`, `JobSettings`, `Dismissals`, `CSSColor`, `IsValidCSSColor`, `EffectiveJobSettings`, `DockerDashboardSections`, `HardwareSections`, `AbsolutePath`, `SnapshotForUser`, `UpdateForUser`, `OpenUserStore`, `Initialize`, plus the `clone*` helpers.

### 6.4 Import-path updates

The set of files to update is whatever `rg 'handlers/config' backend/` matches at migration time, including:

- production code outside `handlers/config/` (docker, filebrowser, jobs, system, register.go, main.go),
- **test files** (e.g., [`system/health_test.go`](../backend/bridge/handlers/system/health_test.go)),
- the **code generator** itself ([`config/generator.go`](../backend/bridge/handlers/config/generator.go), which has `//go:build ignore` and imports `handlers/config` — it must follow the package to its new location and update its own self-import),
- `handlers/config/handlers.go` (the only file remaining in `handlers/config/`, which now imports `bridge/settings`).

Do not encode a fixed file count here; future contributors will add or remove imports. The migration is complete when `rg 'handlers/config' backend/` returns no production references and the build/tests pass.

The transformation is mechanical: `handlers/config` → `bridge/settings`, `config.X` → `settings.X`.

### 6.5 No external imports

Confirmed: nothing in `backend/webserver/` or `backend/common/` imports `handlers/config`. The frontend type generation does not depend on Go package paths. The move has no external blast radius.

---

## 7. Privilege Mechanism

No new mechanism. `privilege.RequirePrivilegedIPC` (the 8-line wrapper) stays exactly as it is. What changes is the **call site**: instead of every package importing `privilege` and writing the wrap inline, `rpc.Register` does it based on `Command.Privileged`.

```go
// rpc/rpc.go (excerpt)
func Register(component string, rt runtime.Runtime, commands []Command) {
    for _, cmd := range commands {
        h := cmd.Handler
        if cmd.Privileged {
            h = privilege.RequirePrivilegedIPC(rt.Session, h)
        }
        ipc.RegisterFunc(component, cmd.Name, h)
    }
}
```

`PRIVILEGE_PATTERN.md` continues to be authoritative for the policy. This ADR only standardizes how the wrap is applied.

After migration, three packages (`indexer`, `power`, `system`) drop their direct import of `privilege` — `rpc.Register` handles it.

---

## 8. Test Strategy

`Runtime` is a concrete struct, **not an interface**. Tests construct one directly with non-nil fields (the `New` constructor's nil-checks are for the production path; tests use struct literals).

### 8.1 Test fixture for `UserStore`

The store fixture lives in a sibling subpackage so it can be imported across packages without polluting the production `settings` import surface and without the cross-package test-file import limitation:

```
backend/bridge/settings/
backend/bridge/settings/settingstest/
    store.go                 // exports NewStore(t testing.TB) *settings.UserStore
```

`settingstest/store.go` is normal production code (not `_test.go`). It imports `testing` so callers from other packages' tests can use it. Production binaries that don't import `settingstest` won't pull in `testing` transitively. This is the standard Go pattern (see e.g. `httptest`, `iotest`).

### 8.2 Recommended test shape

Pick a handler whose business function is testable without external side effects — typically one that only reads from or writes to the store. Example using `list_auto_update_containers` (reads `cfg.Docker.AutoUpdateStacks`):

```go
import (
    "github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
    "github.com/mordilloSan/LinuxIO/backend/bridge/settings/settingstest"
)

func TestListAutoUpdateContainers(t *testing.T) {
    store := settingstest.NewStore(t)
    settingstest.UpdateAutoUpdateStacks(t, store, []string{"foo", "bar"})

    rt := runtime.Runtime{
        Session: &session.Session{User: session.User{Username: "alice"}},
        Store:   store,
    }
    h := dockerHandlers{rt: rt}

    emit := &fakeEmit{}
    if err := h.listAutoUpdateContainers(t.Context(), nil, emit); err != nil {
        t.Fatalf("err = %v", err)
    }
    if got := emit.result.([]string); !slices.Equal(got, []string{"foo", "bar"}) {
        t.Fatalf("result = %v", got)
    }
}
```

### 8.3 Handlers that hit external systems

For handlers that shell out to Docker, dbus, the network, etc. (`startContainer`, `enableConnection`, `mountNFS`), end-to-end unit tests are not the goal. Either:

- skip writing replacement tests at the handler layer — the handler is a thin wrapper over a business function; test the business function directly with whatever scaffolding it already has, **or**
- inject the dependency at the business-function boundary (each package decides; this ADR does not prescribe a fake-injection pattern).

### 8.4 Old adapter tests

Existing `*_test.go` files that test adapter machinery (e.g., `TestDockerNoArgCallWithContextPassesContext` in [`docker/handlers_test.go`](../backend/bridge/handlers/docker/handlers_test.go)) are deleted in their package's migration — they test scaffolding that no longer exists.

---

## 9. What Is NOT Changing

| Area | Status |
|---|---|
| IPC frame protocol, opcodes, payload limits | unchanged |
| `ipc.Handler`, `ipc.HandlerFunc` | unchanged |
| `ipc.Events` interface | unchanged |
| `ipc.RegisterFunc`, `ipc.Register`, `ipc.Get` | unchanged |
| `generic.HandleBridgeStream` | unchanged |
| `privilege.RequirePrivilegedIPC` | unchanged (call site moves) |
| Stream type names (`bridge`, `terminal`, `container`, `docker-logs`, etc.) | unchanged |
| Frontend → bridge wire contract | unchanged |
| `bridge-handler-api.md` | still authoritative for the wire protocol; its stale handler inventory is a doc cleanup follow-up |

### 9.1 Handler Legacy Audit

Audit date: 2026-05-10. This pass checked backend handler registrations, stream registrations, frontend stream openers, and bridge docs for legacy APIs.

| Area | Status | Action |
|---|---|---|
| `terminal.bash` / `terminal.sh` bidirectional RPC | dead registered API; no frontend caller; raw `terminal` and `container` streams are the active path | removed now |
| `ipc.BidirectionalHandler` and `generic.handleBidirectional` | dead compatibility infrastructure after terminal cleanup | removed now |
| Raw stream registry | active streams are `bridge`, `app-update`, `terminal`, `container`, `jobs-attach`, `jobs-data`, `jobs-events`, `general-logs`, `service-logs`, `docker-logs` | migrate signature in step 5 |
| `bridge-handler-api.md` and `frontend-api.md` stream lists | contain stale names (`docker-compose`, `docker-indexer`, `fb-upload`, `fb-download`, `fb-archive`, `pkg-update`, etc.) that are now job types, frontend concepts, or old docs | doc cleanup follow-up; not part of this ADR migration |
| `handlers.Dependencies` | currently only carries `ConfigStore`; exists because runtime is split across parameters | delete when `runtime.Runtime` lands |
| `jobs.RegisterStreamHandlers(..., store)` / `HandleEventsStreamWithStore` | live compatibility shape; binds store at registration while other streams receive session at execution | replace with runtime-at-execution stream signature |
| `docker` adapters and `*WithStore` names | live but legacy style: 8 adapters, 12 factories, 10 log one-liners, adapter tests | delete during docker migration |
| `dbus` adapters/factories | live but legacy style: result/action adapters plus one-off JSON-ish factories | delete during dbus migration |
| `filebrowser` emit adapters and store-bound job runner closures | live but legacy style; also has job types rather than raw `fb-*` streams | replace with receiver handlers and runtime-backed job registration |
| `system` emit adapters and closure factories | live but legacy style; privilege flag already points at the target registration model | replace with receiver handlers and `rpc.Command.Privileged` |
| `power` / `indexer` direct privilege wrappers | active and correct today, but package-local boilerplate | move privilege wrapping to `rpc.Register` |
| `wireguard` local registration closure with logging skip-list | active but awkward local mini-adapter | flatten into explicit handlers or migrate through shared helpers |
| `accounts`, `control`, `storage`, `shares` | no dead compatibility API found; style ranges from clean to verbose | migrate mechanically to shared helpers |
| `loginhistory` / `generic` | not handler packages in this ADR's sense; `loginhistory` is a system helper, `generic` is the dispatcher | leave alone except for runtime stream signature where applicable |

---

## 10. Migration Order

Each step ends with `go build ./... && go test ./...` green.

1. **This ADR lands.** Code follows; no exceptions.
2. **`bridge/settings/`** — move the config-domain files, update every `handlers/config` import found by `rg`, and apply the mechanical `config.X` → `settings.X` rename. *(One package; no behavior change.)*
3. **`bridge/runtime/`** — new package, ~30 lines.
4. **`handlers/internal/rpc/`** — new package, ~80 lines.
5. **Stream registry signature change** — `streamHandlers map[string]func(runtime.Runtime, net.Conn, []string) error`. Update 4 packages (`control`, `terminal`, `logs`, `jobs`). Drop `handlers.Dependencies`.
6. **Migrate clean packages** (smallest deltas, prove the pattern):
   - `accounts`
   - `control`
   - `power` (drops direct privilege import)
   - `wireguard` (drops the skip-list logging hack)
   - `indexer` (drops direct privilege import)
   - `jobs`
7. **Migrate verbose packages** (collapse inline error logging through `rpc.EmitResult`):
   - `shares`
   - `storage`
8. **Migrate mixed packages**:
   - `filebrowser` (drop `emitFilebrowserArgsResult`/`emitFilebrowserLoggedArgsResult`)
   - `system` (drop `emitSystemCall`/`emitSystemArgCall`/`emitSystemResult`; the `privileged: true` flag generalizes via `rpc.Command.Privileged`)
   - `config` (handlers only; the move in step 2 already happened)
9. **Migrate adapter-heavy packages**:
   - `dbus` (drop 4 adapters + 12 factories)
   - `docker` (drop 8 adapters + 12 factories + 10 log fns; apply the file split: `handlers.go`, `handlers_containers.go`, `handlers_compose.go`, `handlers_icons.go`, `handlers_caddy.go`, `handlers_info_prune.go`)
10. **Cleanup sweep.** `grep -r 'NoArgCall\|OneArgCall\|UserCall\|LoggedCall\|emitSystemCall\|emitFilebrowser\|dbusNoArgResult\|dbusOneArgResult\|dbusNoArgAction\|oneArgActionHandler' backend/` should return nothing.

Each step is self-contained: passes its own tests, doesn't break others.

---

## 11. Backwards-Compatibility Policy

**No long-lived compatibility wrappers.** Each package's migration deletes the old shape in the same change that introduces the new one. The reasons:

- The `*WithStore` suffix (in `docker`, `filebrowser`) was originally introduced as a compat wrapper. The non-`WithStore` versions never came back, but the awkward names persist. That's the failure mode this rule prevents.
- During a migration, `git checkout` is the rollback. There's no need to keep both shapes alive in the tree.
- Atomic per-package migration means at most one package is half-done at a time.

Consequence: if a migration breaks an unrelated caller, the migration is incomplete and not yet merge-ready. No "we'll update the caller in the next PR."

---

## 12. Answers to the Open Questions

These were the eight design questions raised before research. Answers below are now the locked-in decisions.

**Q1 — Where does config domain code go?**
`backend/bridge/settings/`. Rename `config.X` → `settings.X`. `handlers/config/handlers.go` stays put.

**Q2 — Does `rpc.Register` wrap or replace `ipc.RegisterFunc`?**
Wraps. The IPC layer is clean and not the problem; replacing it would inflate scope without benefit.

**Q3 — How does privilege map?**
`Command{Privileged: bool}` at registration. `rpc.Register` calls `privilege.RequirePrivilegedIPC(rt.Session, handler)` when set. The privilege package itself is unchanged.

**Q4 — Should `Runtime` always be passed?**
Yes, even to packages that don't use it. Uniform `RegisterHandlers(rt runtime.Runtime)` across all packages outweighs the cost of unused fields.

**Q5 — Testing strategy?**
`Runtime` is a concrete struct. Tests build one with a real (or test-fixture) `Session` and `UserStore`. No interfaces. `bridge/settings/settingstest.NewStore(t)` provides a clean fixture.

**Q6 — Bidirectional handlers?**
Removed for bridge RPC. The legacy `terminal.bash` / `terminal.sh` handlers were removed because the active frontend path uses raw streams. Future terminal-style protocols should use raw streams unless there is a new, concrete reason to rebuild bidirectional RPC support.

**Q7 — Stream handler registry shape?**
`map[string]func(runtime.Runtime, net.Conn, []string) error`. All four producer packages (`control`, `terminal`, `logs`, `jobs`) update their `RegisterStreamHandlers` signature. The `Dependencies` struct is deleted.

**Q8 — Unaudited packages?**
All 16 handler packages were read. `loginhistory` is a library (no `RegisterHandlers`), used by `system`. `generic` is the bridge stream dispatcher (`HandleBridgeStream`), not a handler package. Both are unaffected by this ADR.

---

## 13. Concrete Examples Per Package (Post-Migration Sketch)

Quick visual of what each package looks like under the new pattern. Full migration is per step 10.

### accounts (stateless, was already clean)

```go
func RegisterHandlers(rt runtime.Runtime) {
    rpc.Register("accounts", rt, []rpc.Command{
        {Name: "list_users", Handler: handleListUsers},
        // ...
    })
}

func handleListUsers(ctx context.Context, args []string, emit ipc.Events) error {
    return rpc.EmitResult(emit, ListUsers())
}
```

Net change: ~20 lines, mostly removing custom `requireAccountArgs`/`decodeAccountJSON`/`emitAccountResult` helpers in favor of the shared `rpc` ones.

### docker (stateful)

```go
type dockerHandlers struct {
    rt runtime.Runtime
}

func RegisterHandlers(rt runtime.Runtime) {
    h := dockerHandlers{rt: rt}
    rpc.Register("docker", rt, []rpc.Command{
        {Name: "list_containers", Handler: h.listContainers},
        {Name: "compose_up",      Handler: h.composeUp},
        // 40 more
    })
}
```

Net change: deletes 8 adapters + 12 factories + 10 log fns. Methods move to `handlers_containers.go`, `handlers_compose.go`, `handlers_icons.go`, `handlers_caddy.go`, `handlers_info_prune.go` (split per the docker plan we already designed).

### system (privileged-mixed)

```go
func RegisterHandlers(rt runtime.Runtime) {
    h := systemHandlers{rt: rt}
    rpc.Register("system", rt, []rpc.Command{
        {Name: "get_cpu_info",                Handler: handleGetCPUInfo},
        {Name: "list_failed_login_events",    Handler: h.listFailedLoginEvents, Privileged: true},
        {Name: "dismiss_unclean_shutdown",    Handler: h.dismissUncleanShutdown},
        // ...
    })
}
```

Net change: drops `emitSystemCall`/`emitSystemArgCall`/`emitSystemResult`. The existing `privileged: true` flag in its registration struct generalizes seamlessly to `rpc.Command.Privileged`.

### terminal (stateless RPC + raw streams)

```go
func RegisterHandlers(rt runtime.Runtime) {
    rpc.Register("terminal", rt, []rpc.Command{
        {Name: "list_shells", Handler: handleListShells},
    })
}
```

Net change: the unused bidirectional `terminal.bash` / `terminal.sh` registrations and private `terminalHandler` implementation are gone. The raw stream types `terminal` and `container` remain the canonical terminal protocols.

---

## 14. Approval

This ADR is the gate. No code that touches a handler package merges before it lands. Once approved, the migration in section 10 begins; before then, all in-flight handler work is paused.
