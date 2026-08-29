# Core Config Quarantine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A corrupt `~/.linuxio-config.yaml` no longer locks the user out of LinuxIO: at bridge startup it is moved aside and replaced with defaults, and the bridge logs where every config artifact lives and when it creates or resets one.

**Architecture:** One new boot-only loader in `backend/bridge/internal/config/store.go` wraps the existing `readCoreLatestOwned`. Parse/validation failures (identified by a new sentinel error) trigger rename-to-`.broken-<timestamp>` + write-defaults; every other failure (symlink, directory, I/O) still aborts startup. The mutation path (`UserStore.Update`) keeps calling `readCoreLatestOwned` directly and keeps refusing to touch an unreadable file. Three log lines are added where files are created, reset, or announced.

**Tech Stack:** Go (stdlib `os`, `errors`, `log/slog`, `time`), `github.com/goccy/go-yaml`, `testify/require`. Backend only.

**Spec:** Inline — see *Design* below. Derived from the 2026-08-29 review of `backend/bridge/cmd/root.go` and `backend/bridge/internal/config/`.

## Global Constraints

- **Never create, amend, or push a Git commit.** This plan has no commit steps. Leave commit creation to the user; a suggested commit message is at the end.
- **Run only repository Make targets.** Never `go test`, `gofmt`, `golangci-lint` directly. Focused runs use `make test-backend-quiet GO_TEST_FLAGS="-run '<regex>' -count=1"`; the final gate is `make check-backend-quiet`. Quiet targets keep full logs in `.cache/test-logs/` — read those before rerunning with normal output.
- **Never run two Make invocations concurrently**, and never run Make while an edit is in flight. Inspect `git status` / `git diff` after every Make run: lint and modernize can mutate the worktree.
- **Go style (CLAUDE.md):** synchronous explicit control flow; wrap errors with operation context and `%w`; no panics on runtime/input failures; stdlib over new deps.
- Every test in this plan runs unprivileged in `t.TempDir()`. Do not add tests that need root, a real passwd home, systemd, or Docker.
- The dev host runs a live LinuxIO; do not touch `~/.linuxio-config.yaml` on the host to "test manually".

---

## Design

### Current behavior (source-verified)

Boot order, `backend/bridge/cmd/root.go:45-95` (`runBridgeProcess`):
1. logging + debug server → 2. `initializeBridgeSession()` → 3. `openClientConnection()` → 4. **`config.OpenUserStore(username, uid, gid)`** (`root.go:84`) → 5. `runtime.New` → 6. `runBridge` → handlers → ready handoff.

`OpenUserStore` (`store.go:39-77`) holds both sidecar locks and calls, in order: `initializeLockedOwned` (`init.go:24-50`: writes `DefaultSettings(base)` if the core file is **missing**, `{}` if the UI file is missing), then `readCoreLatestOwned` and `readUILatestOwned`.

- Core file **corrupt** (YAML syntax, unknown field under `yaml.Strict()`, `ValidateConfig` failure, empty or multi-document): `readCoreLatestOwned` returns `fmt.Errorf("invalid core config: %w", parseErr)`, `OpenUserStore` fails, the bridge reports `bridge config store failed: …` over the status fd and exits. User cannot log in.
- UI file corrupt: `readUILatestOwned` (`store.go:282-299`) silently overwrites it with `{}` and continues.
- Log coverage: `root.go:90` logs the core path only. `logYAMLError` logs parse failures with path/line/column. Default-file creation and the UI reset are **not** logged.

### Target behavior

| Situation at boot | Before | After |
|---|---|---|
| Core file missing | defaults written silently | defaults written, `slog.Info` with path |
| Core file fails parse/validation | bridge exits | file renamed to `<path>.broken-<UTC ts>`, defaults written, `slog.Warn` with both paths + error, bridge starts |
| Core path is symlink / directory / unreadable | bridge exits | unchanged — bridge exits, nothing renamed or written |
| UI file missing | `{}` written silently | `{}` written, `slog.Info` with path |
| UI file corrupt | reset to `{}` silently | reset to `{}`, `slog.Warn` with path + error |
| `config store ready` log | core path | core path **and** UI path |
| `UserStore.Update` on corrupt core | fails, file untouched | **unchanged** — fails, file untouched, nothing quarantined |

Why quarantine rather than overwrite: `docker.folders` points at real compose stacks and `requireMountsForFolders` drives a systemd drop-in. Losing that silently would be worse than the outage. The `.broken-*` copy lets the user diff and restore. Why only at boot: a mutation that finds an unreadable file must not "fix" it by discarding it — the existing tests `TestCore*DoesNotRewrite` and `TestUserStoreMutationRejectsMalformedCoreWithoutRewriting` encode that and stay green.

Concurrency: quarantine runs inside `withConfigLocksOwned`, so two bridges for the same user cannot both quarantine — the second sees the fresh defaults. `os.Rename` within one directory is atomic and preserves the inode's ownership, so no chown is needed on the quarantined file.

### File map

| File | Change |
|---|---|
| `backend/bridge/internal/config/store.go` | add `errInvalidCoreConfig`; wrap parse failure in `readCoreLatestOwned`; add `loadCoreOrQuarantineOwned`; call it from `OpenUserStore`; `slog.Warn` in `readUILatestOwned` reset path |
| `backend/bridge/internal/config/store_test.go` | new tests for `loadCoreOrQuarantineOwned`; one extra assertion in `TestUserStoreMutationRejectsMalformedCoreWithoutRewriting` |
| `backend/bridge/internal/config/init.go` | two `slog.Info` lines when defaults are created |
| `backend/bridge/internal/config/validator.go` | doc comment on `parseCoreConfig` |
| `backend/bridge/cmd/root.go` | add `ui_path` to the `config store ready` log |
| `docs/api-contract.md` | replace the "fails without a reset" paragraph |

---

### Task 1: Boot-time quarantine loader

**Files:**
- Modify: `backend/bridge/internal/config/store.go:263-279` (`readCoreLatestOwned`) and imports
- Test: `backend/bridge/internal/config/store_test.go`

**Interfaces:**
- Consumes: `readCoreLatestOwned(path, base string) (*Settings, error)`, `writeCoreConfigOwned(cfgPath string, cfg Settings, owner fileOwnership) error`, `DefaultSettings(base string) *Settings`, `fileOwnership` (all existing, same package).
- Produces: `var errInvalidCoreConfig error` (sentinel, unexported) and `func loadCoreOrQuarantineOwned(path, base string, owner fileOwnership) (*Settings, error)`. Task 2 wires the latter into `OpenUserStore`.
- Test helpers already available in package `config` tests: `readConfigStrict(path) (*Settings, error)` (`settings_test.go:12`), `writeCoreConfig(cfgPath, cfg)` (`test_helpers_test.go`), `currentProcessFileOwnership()` (`test_helpers_test.go`), constants `cfgFileName`, `filePerm`, `dirPerm`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/bridge/internal/config/store_test.go` (imports `context`, `os`, `path/filepath`, `sync`, `testing`, `require` already present):

```go
func quarantinedCoreFiles(t *testing.T, cfgPath string) []string {
	t.Helper()
	matches, err := filepath.Glob(cfgPath + ".broken-*")
	require.NoError(t, err)
	return matches
}

func TestLoadCoreOrQuarantineKeepsValidCore(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	cfg := DefaultSettings(base)
	cfg.Docker.Folders = []AbsolutePath{"/srv/linuxio-projects"}
	require.NoError(t, writeCoreConfig(cfgPath, *cfg))
	before, err := os.ReadFile(cfgPath)
	require.NoError(t, err)

	loaded, err := loadCoreOrQuarantineOwned(cfgPath, base, currentProcessFileOwnership())
	require.NoError(t, err)
	require.Equal(t, cfg, loaded)
	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Empty(t, quarantinedCoreFiles(t, cfgPath))
}

func TestLoadCoreOrQuarantineReplacesInvalidCore(t *testing.T) {
	tests := map[string]string{
		"syntax error":       "docker: [broken",
		"unknown field":      "docker:\n  folders: [/srv/projects]\nunknown: true\n",
		"semantic failure":   "appSettings:\n  chunkSizeMB: 33\ndocker:\n  folders: [/srv/projects]\n",
		"empty document":     "# comments only\n",
		"typed path failure": "docker:\n  folders: [relative]\n",
		"multiple documents": "{}\n---\n{}\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			cfgPath := filepath.Join(base, cfgFileName)
			require.NoError(t, os.WriteFile(cfgPath, []byte(contents), filePerm))

			loaded, err := loadCoreOrQuarantineOwned(cfgPath, base, currentProcessFileOwnership())
			require.NoError(t, err)
			require.Equal(t, DefaultSettings(base), loaded)

			persisted, err := readConfigStrict(cfgPath)
			require.NoError(t, err)
			require.Equal(t, DefaultSettings(base), persisted)

			quarantined := quarantinedCoreFiles(t, cfgPath)
			require.Len(t, quarantined, 1)
			raw, err := os.ReadFile(quarantined[0])
			require.NoError(t, err)
			require.Equal(t, []byte(contents), raw)
		})
	}
}

func TestLoadCoreOrQuarantineDoesNotTouchNonParseFailures(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		base := t.TempDir()
		cfgPath := filepath.Join(base, cfgFileName)
		target := filepath.Join(base, "real.yaml")
		require.NoError(t, os.WriteFile(target, []byte("docker: [broken"), filePerm))
		require.NoError(t, os.Symlink(target, cfgPath))

		_, err := loadCoreOrQuarantineOwned(cfgPath, base, currentProcessFileOwnership())
		require.ErrorContains(t, err, "symlink")
		info, err := os.Lstat(cfgPath)
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&os.ModeSymlink)
		require.Empty(t, quarantinedCoreFiles(t, cfgPath))
	})
	t.Run("directory", func(t *testing.T) {
		base := t.TempDir()
		cfgPath := filepath.Join(base, cfgFileName)
		require.NoError(t, os.Mkdir(cfgPath, dirPerm))

		_, err := loadCoreOrQuarantineOwned(cfgPath, base, currentProcessFileOwnership())
		require.ErrorContains(t, err, "not a regular file")
		require.Empty(t, quarantinedCoreFiles(t, cfgPath))
	})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-backend-quiet GO_TEST_FLAGS="-run 'TestLoadCoreOrQuarantine' -count=1"`
Expected: FAIL — compile error in package `config` tests: `undefined: loadCoreOrQuarantineOwned`. (The quiet target also runs `test-auth*`; those are unrelated and should pass. If the quiet summary is unclear, read `.cache/test-logs/`.)

- [ ] **Step 3: Implement the sentinel and loader**

In `backend/bridge/internal/config/store.go`, extend imports to:

```go
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)
```

Replace `readCoreLatestOwned` (currently `store.go:263-279`) with:

```go
// errInvalidCoreConfig marks a core document that exists and was read but
// failed to decode or validate. The boot-time loader quarantines only this
// class of failure; symlink, type, and I/O failures are never repaired.
var errInvalidCoreConfig = errors.New("invalid core config")

func readCoreLatestOwned(path, base string) (*Settings, error) {
	exists, err := CheckConfig(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("core config path is not a regular file: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, parseErr := parseCoreConfig(raw, path, base)
	if parseErr == nil {
		return cfg, nil
	}
	return nil, fmt.Errorf("%w: %w", errInvalidCoreConfig, parseErr)
}

// loadCoreOrQuarantineOwned is the boot-time core read. A document that fails
// to decode or validate is moved to <path>.broken-<UTC timestamp> and replaced
// with defaults so one bad edit, or a downgrade past an unknown field, cannot
// lock the user out; the original stays on disk for manual recovery. Every
// other failure is returned unchanged. UserStore.Update never calls this: a
// mutation must not reset a file it could not read.
func loadCoreOrQuarantineOwned(path, base string, owner fileOwnership) (*Settings, error) {
	cfg, err := readCoreLatestOwned(path, base)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, errInvalidCoreConfig) {
		return nil, err
	}
	quarantinePath := path + ".broken-" + time.Now().UTC().Format("20060102T150405Z")
	if renameErr := os.Rename(path, quarantinePath); renameErr != nil {
		return nil, errors.Join(err, fmt.Errorf("quarantine core config: %w", renameErr))
	}
	defaults := DefaultSettings(base)
	if writeErr := writeCoreConfigOwned(path, *defaults, owner); writeErr != nil {
		return nil, errors.Join(err, fmt.Errorf("write default core config: %w", writeErr))
	}
	slog.Warn("core config quarantined, defaults written",
		"component", "config",
		"path", path,
		"quarantined_path", quarantinePath,
		"error", err,
	)
	return defaults, nil
}
```

Notes for the implementer: the error string of `readCoreLatestOwned` is unchanged (`invalid core config: <detail>`), so `TestCoreReadFailurePreservesInvalidDocument` (`settings_test.go:167`) still passes. `fmt.Errorf` with two `%w` verbs is supported since Go 1.20 and `errors.Is` traverses both.

- [ ] **Step 4: Run the new tests and the existing no-rewrite tests**

Run: `make test-backend-quiet GO_TEST_FLAGS="-run 'TestLoadCoreOrQuarantine|TestCore.*Rewrite|TestCoreReadFailurePreservesInvalidDocument|TestInvalidCoreDoesNotClobberValidUI' -count=1"`
Expected: PASS for all. The pre-existing `TestCore*DoesNotRewrite` tests must remain green — they exercise `readCoreLatestOwned`, which still never writes.

- [ ] **Step 5: Check the worktree**

Run: `git status --short backend/`
Expected: only `store.go` and `store_test.go` modified. If Make tooling touched anything else, inspect the diff and keep or revert deliberately; report it.

---

### Task 2: Wire the loader into startup and lock down the mutation path

**Files:**
- Modify: `backend/bridge/internal/config/store.go:62` (inside `OpenUserStore`'s `withConfigLocksOwned` closure)
- Test: `backend/bridge/internal/config/store_test.go:167-184` (`TestUserStoreMutationRejectsMalformedCoreWithoutRewriting`)

**Interfaces:**
- Consumes: `loadCoreOrQuarantineOwned(path, base string, owner fileOwnership) (*Settings, error)` from Task 1; `quarantinedCoreFiles(t, cfgPath)` test helper from Task 1.
- Produces: nothing new. `OpenUserStore`'s signature is unchanged.

- [ ] **Step 1: Strengthen the mutation test**

In `TestUserStoreMutationRejectsMalformedCoreWithoutRewriting`, add one line after `require.Equal(t, coreRaw, rewritten)`:

```go
	require.Empty(t, quarantinedCoreFiles(t, cfgPath))
```

- [ ] **Step 2: Run it to confirm it passes before the wiring change**

Run: `make test-backend-quiet GO_TEST_FLAGS="-run 'TestUserStoreMutationRejectsMalformedCoreWithoutRewriting' -count=1"`
Expected: PASS. (This is a guard, not a red test: it must stay green after Step 3, proving `Update` does not quarantine.)

- [ ] **Step 3: Switch `OpenUserStore` to the quarantining loader**

In `store.go`, inside `OpenUserStore`, change

```go
		cfg, err := readCoreLatestOwned(cfgPath, base)
```

to

```go
		cfg, err := loadCoreOrQuarantineOwned(cfgPath, base, owner)
```

Also update the `OpenUserStore` doc comment to:

```go
// OpenUserStore prepares both files and loads both snapshots while holding both
// sidecar locks. A core document that fails to decode or validate is
// quarantined and replaced with defaults (see loadCoreOrQuarantineOwned); an
// invalid UI document is reset to {}. The authenticated numeric UID/GID identify
// the owner of every runtime artifact; username is used only to resolve the
// configuration base.
```

- [ ] **Step 4: Run the config package tests**

Run: `make test-backend-quiet GO_TEST_FLAGS="-run 'TestUserStore|TestLoadCore|TestCore|TestOwned|TestInitialize|TestInvalid' -count=1"`
Expected: PASS. `TestUserStoreMutationRejectsMalformedCoreWithoutRewriting` still green (Update untouched); `TestOwnedConfigArtifactsUseTargetOwnership` still green (ownership path unchanged).

Why there is no `OpenUserStore`-level test: it resolves the real passwd home via `Homedir(username)` and verifies its owner, so an unprivileged unit test cannot point it at `t.TempDir()`. Coverage of the quarantine branch is at `loadCoreOrQuarantineOwned`; the wiring is a one-line call site.

- [ ] **Step 5: Check the worktree**

Run: `git status --short`
Expected: `store.go`, `store_test.go` only (plus Task 1 changes).

---

### Task 3: Log file creation, resets, and both paths at ready

**Files:**
- Modify: `backend/bridge/internal/config/init.go:39-50` (`initializeLockedOwned`)
- Modify: `backend/bridge/internal/config/store.go` (`readUILatestOwned`, the reset branch)
- Modify: `backend/bridge/cmd/root.go:90`

**Interfaces:**
- Consumes: `UserStore.UIPath() string` (existing, `store.go:86`).
- Produces: nothing; log-only.

No unit test asserts on slog output in this package, and the existing tests already cover the branches these lines sit in (`TestInitializeCreatesMissingFilesIndependently`, `TestUI*ResetsWholeDocument`). Do not add a log-capture harness.

- [ ] **Step 1: Log default-file creation in `initializeLockedOwned`**

`init.go` already imports `log/slog`. Change the two creation branches to:

```go
	if !coreExists {
		if err := writeCoreConfigOwned(cfgPath, *DefaultSettings(base), owner); err != nil {
			return fmt.Errorf("write default core config: %w", err)
		}
		slog.Info("wrote default core config", "component", "config", "path", cfgPath)
	} else if err := owner.ensureFile(cfgPath); err != nil {
		return fmt.Errorf("own core config: %w", err)
	}
	if !uiExists {
		if err := writeEmptyUIConfigOwned(uiPath, owner); err != nil {
			return fmt.Errorf("write default UI config: %w", err)
		}
		slog.Info("wrote empty UI config", "component", "config", "path", uiPath)
	} else if err := owner.ensureFile(uiPath); err != nil {
		return fmt.Errorf("own UI config: %w", err)
	}
```

- [ ] **Step 2: Log the UI reset in `readUILatestOwned`**

In `store.go`, `readUILatestOwned`, the tail currently reads:

```go
	replacement := DefaultUIPreferences()
	if err := writeEmptyUIConfigOwned(path, owner); err != nil {
		return nil, errors.Join(
			fmt.Errorf("invalid UI config: %w", parseErr),
			fmt.Errorf("reset UI config: %w", err),
		)
	}
	return &replacement, nil
```

Change to:

```go
	replacement := DefaultUIPreferences()
	if err := writeEmptyUIConfigOwned(path, owner); err != nil {
		return nil, errors.Join(
			fmt.Errorf("invalid UI config: %w", parseErr),
			fmt.Errorf("reset UI config: %w", err),
		)
	}
	slog.Warn("UI config reset to defaults", "component", "config", "path", path, "error", parseErr)
	return &replacement, nil
```

- [ ] **Step 3: Log both paths at ready**

In `backend/bridge/cmd/root.go:90`, change

```go
	slog.Info("config store ready", "user", sess.User.Username, "path", userConfig.Path())
```

to

```go
	slog.Info("config store ready", "user", sess.User.Username, "path", userConfig.Path(), "ui_path", userConfig.UIPath())
```

- [ ] **Step 4: Build-check via the focused test run**

Run: `make test-backend-quiet GO_TEST_FLAGS="-run 'TestInitializeCreatesMissingFilesIndependently|TestUI.*ResetsWholeDocument|TestUserStoreUIReplacementDoesNotReadMalformedOldSnapshot' -count=1"`
Expected: PASS. Compiles `backend/bridge/cmd` too (part of `./...`), which verifies the `root.go` edit.

- [ ] **Step 5: Check the worktree**

Run: `git status --short`
Expected: `init.go`, `store.go`, `store_test.go`, `root.go`.

---

### Task 4: Reconcile documentation and code comments

**Files:**
- Modify: `docs/api-contract.md:387-390`
- Modify: `backend/bridge/internal/config/validator.go:16-19` (`parseCoreConfig` doc comment)

**Interfaces:** none.

- [ ] **Step 1: Update the contract doc**

In `docs/api-contract.md`, replace the paragraph

```
Pre-split combined `.linuxio-config.yaml` files are no longer accepted. Strict
core decoding rejects their UI fields and leaves the file untouched, so startup
fails; current malformed core content likewise fails without a reset. The
bridge never uses filesystem observations to repair Docker folders.
```

with

```
Pre-split combined `.linuxio-config.yaml` files are no longer accepted, and
strict core decoding rejects unknown fields. At bridge startup a core document
that fails to decode or validate is renamed to
`.linuxio-config.yaml.broken-<UTC timestamp>` and replaced with defaults; the
bridge logs both paths at warning level and starts. Read, stat, and symlink
failures still abort startup and touch nothing. A core update that finds an
unreadable document fails and leaves the file untouched; only the startup read
quarantines. The bridge logs the core and UI paths when the store is ready and
logs every default-file creation and UI reset. The bridge never uses filesystem
observations to repair Docker folders.
```

- [ ] **Step 2: Update the `parseCoreConfig` comment**

In `validator.go`, replace

```go
// parseCoreConfig decodes one complete core document and validates the result.
// Defaults only supply omitted core fields. A failed decode is returned to the
// caller unchanged so a malformed or unknown-field document cannot be erased
// by a read or an unrelated mutation.
```

with

```go
// parseCoreConfig decodes one complete core document and validates the result.
// Defaults only supply omitted core fields. A failed decode is returned to the
// caller unchanged; parseCoreConfig never rewrites. Only the startup read
// (loadCoreOrQuarantineOwned) quarantines and replaces an invalid document;
// core mutations leave it untouched.
```

- [ ] **Step 3: Confirm no other doc claims the old behavior**

Run: `grep -rn "without a reset\|fails without\|cannot be erased" docs backend --include=*.md --include=*.go`
Expected: no matches.

---

### Task 5: Final verification (test worker, no edits)

**Files:** none modified by hand.

Per CLAUDE.md, a fresh test worker runs the gate and reports; it does not fix. Sol reviews the complete diff afterward.

- [ ] **Step 1: Run the backend gate**

Run: `make check-backend-quiet`
Expected: `✅ Backend checks passed!` — golangci-lint, modernize, govulncheck, full `go test ./... -race`, deadcode (informational). On failure, read `.cache/test-logs/` and report the exact failing check and message; do not edit.

- [ ] **Step 2: Report worktree state**

Run: `git status --short && git diff --stat`
Expected changed files, and only these:

```
 backend/bridge/cmd/root.go
 backend/bridge/internal/config/init.go
 backend/bridge/internal/config/store.go
 backend/bridge/internal/config/store_test.go
 backend/bridge/internal/config/validator.go
 docs/api-contract.md
 docs/superpowers/plans/2026-08-29-core-config-quarantine.md
```

Report any file changed automatically by tooling (formatter, `go mod tidy`, modernize) — Sol decides whether to keep it.

- [ ] **Step 3: Sol reviews the full diff**

Checklist for the reviewer:
- `UserStore.Update` still calls `readCoreLatestOwned`, not the quarantining loader.
- `loadCoreOrQuarantineOwned` returns non-sentinel errors unchanged, before any rename.
- The `readCoreLatestOwned` error string still begins `invalid core config:`.
- No new dependencies; no `go.mod`/`go.sum` changes.
- Log keys use `component=config` like the rest of the package.

---

## Suggested commit message (for the user; do not commit)

```
fix(config): quarantine invalid core config at startup instead of refusing to boot

A ~/.linuxio-config.yaml that fails strict decode or validation is renamed
to .linuxio-config.yaml.broken-<timestamp> and replaced with defaults so the
bridge starts; the original stays for recovery. Symlink, type, and I/O
failures still abort startup, and core mutations still never rewrite an
unreadable file. Log default-file creation, UI resets, and both config paths
at ready.
```

## Self-review notes

- Coverage: every row of the *Target behavior* table maps to a task (rows 1,4 → Task 3 step 1; row 2 → Tasks 1–2; row 3 → Task 1 test `DoesNotTouchNonParseFailures`; row 5 → Task 3 step 2; row 6 → Task 3 step 3; row 7 → Task 2 step 1). Docs → Task 4.
- Names are consistent across tasks: `errInvalidCoreConfig`, `loadCoreOrQuarantineOwned(path, base string, owner fileOwnership) (*Settings, error)`, `quarantinedCoreFiles(t, cfgPath) []string`.
- Deliberately out of scope: an `OpenUserStore`-level integration test (needs a real passwd home); a log-capture test harness; any frontend change (`config.get`/`config.get_ui` payloads are unchanged, so `make generate` is not needed).
