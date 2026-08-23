# Bridge per-user config simplification

> **Status: Implemented.** This document records the current two-file policy
> and the historical defect evidence that led to it. The current policy at the
> top takes precedence over the historical sections below.

## Implemented ownership model

LinuxIO has two bridge-owned, per-user YAML files. There is no embedded
database, JSON configuration, or legacy conversion:

| File | Owner and contents | Failure policy |
|---|---|---|
| `~/.linuxio-config.yaml` | Bridge-owned functional settings and defaults: file-browser and upload behavior, Docker (including `docker.folders`), jobs, and dismissals. | A missing file is created from functional defaults. A decode or validation failure atomically replaces only this file with functional defaults. |
| `~/.linuxio-ui.yaml` | Frontend-produced UI snapshots: theme, navigation, collapsed/hidden state, sections, view modes, and layout orders. The backend owns their default values and validation. | A missing file is created as an empty document. A decode or validation failure atomically replaces only this file with an empty document. |

At bridge login, each file is checked and initialized independently. A valid
core file is loaded as-is; an empty or older sparse UI document is decoded on
top of backend UI defaults without rewriting it. Missing one file never causes
the bridge to read the other file as a migration source, and a bad UI file
cannot reset functional settings (or vice versa). There is no projection,
rename, archive, or other conversion of the former combined configuration. Any
old or unknown content that fails the current strict schema is treated as a
content failure and the corresponding file is reset.

Decode is strict and validation is pure: the bridge either accepts the whole
document or resets that document. It does not use a permissive second decode,
salvage valid siblings, or perform field-by-field repair. Read, permissions,
locking, symlink, and other security failures are operational errors and do not
trigger a reset. A reset write uses the owner-aware atomic writer: failures before its
rename preserve the old bytes; a directory-sync failure reported after rename
has an intentionally uncertain commit result, so startup still fails rather
than claiming the replacement is durable.

Docker-folder validation is structural only. It checks the configuration
invariants (including a non-empty list of absolute, non-root, non-duplicate
paths), but never checks whether a path currently exists or is a directory.
Filesystem usability is runtime state; it is never persisted, repaired, or
mutated by configuration loading.

Both files use independent in-process mutexes and sidecar `flock` files, and
both use atomic whole-file writes. A UI write therefore cannot couple itself to
a Docker-setting write, while concurrent sessions for the same user still
serialize updates. UI snapshots use deliberate last-committed-snapshot semantics:
the lock prevents torn or interleaved files, but it does not merge fields from
two simultaneously open browser sessions. That trade-off is confined to
cheaply recoverable presentation state.

The authenticated bootstrap UID/GID, rather than the bridge process identity,
owns both YAML files, their lock files, and every atomic replacement. The store
rejects a process UID/GID mismatch, resolves only the authenticated user's
passwd home, and verifies that home is user-owned without changing its
ownership. A privileged bridge assigns the target UID/GID to an atomic temp
inode before writing configuration bytes and to a lock fd before taking the
lock; the same owner-aware path is used by an unprivileged bridge. This avoids
root-owned completed artifacts without adding a separate writer process.

The backend is the only source of persisted defaults, including presentation
defaults. `config.get_ui` expands an empty UI document into an effective
snapshot; the frontend renders that response directly and cannot save until it
loads. The first UI change writes one complete frontend-produced snapshot, and
later changes replace it through one ordered save queue. There is no browser
configuration snapshot, theme cache, or default-equality normalization.
Frontend theme design tokens and each component's natural layout remain
presentation implementation, not persisted configuration defaults.

The public boundary changed deliberately: `config.get`/`config.set` now carry
only functional configuration, while `config.get_ui` returns effective UI
configuration and `config.set_ui` replaces the persisted UI snapshot. Omitted
replacement fields resolve to backend defaults. The generated API parity guard
covers both shapes.

This removes the temporary migration and repair machinery from the ownership
contract. The bridge has one coherent reset decision per file and one pure
validation boundary for each persisted schema.

## Historical context

Before this work the package owned exactly one file:
`~/.linuxio-config.yaml`, created and
repaired at `Initialize`, then read once and written through `UserStore`. The
file is machine-owned. It is not documented anywhere as user-editable, its path
appears in only two places in the repository (both in `init.go`), every write
rewrites it in full, and unknown keys are silently stripped.

Related documents:

- [API Contract](../api-contract.md) defines the wire shape the handlers expose;
  the persisted struct is deliberately not that shape.
- [Handler Patterns](../bridge_handler_patterns.md) defines the validation
  boundary that already owns every config mutation.

## Historical baseline (pre-simplification)

The following measurements and source references describe the implementation
before the two-file reset policy. They explain why the old machinery was
removed; they are not requirements for the current implementation.

The package is 1,558 non-test Go lines, of which 1,472 compile into the bridge
(`generator.go` carries `//go:build ignore`). Three validation mechanisms are
layered on top of one another, each correct for a different threat, none
retired:

1. **Typed decode validators** — `PersistedTheme`, `CSSColor`, and
   `AbsolutePath` implement `UnmarshalYAML` (`types.go:199-257`). These are
   fatal and run first.
2. **Whole-document validation** — `ValidateConfig` (`validator.go:384-453`),
   which gates every `UserStore.Update`.
3. **A mutating repair pass** — `repairConfig` and its helpers
   (`validator.go:17-365`), which rewrites the file in place at startup.

Because mechanism 1 runs first and is fatal, most of mechanism 3's value repair
is unreachable for file-loaded data: `repairInvalidConfigValues` can only ever
observe the zero value for `Theme`, `PrimaryColor`, `CSSColor`, and
`AbsolutePath`. The genuinely reachable invalid states are five:
`navigationMode` and `dockTileColors` (plain `string`, no typed validator),
the gradient range integers, the docker folder list, and negative job integers.

The repair layer is also unpinned. Replaying every `repairConfig` fixture in
`settings_test.go` with `repairInvalidConfigValues` deleted, and again with
`repairDockerFolderPaths` deleted, produces byte-identical output in all five
cases. Both stages were candidates for removal at the time of analysis and the
suite stayed green.

## Historical ownership analysis

This analysis supported the old phased plan. The current ownership and failure
rules are defined in [Implemented ownership model](#implemented-ownership-model)
above.

Three threats, three correct owners. Only two of the threats are real.

| Threat | Reachable | Correct owner |
|---|---|---|
| Torn or partial write | No | `utils.WriteFileAtomic` plus `updateMu` and the sidecar flock already in `store.go:130-137`. No validation mechanism is aimed here. |
| Externally modified or corrupted file | Yes | Fatal-but-recoverable parse: refuse the value, log it with position, write defaults, **stop**. Mechanisms 1 and 2. |
| Upgrade adds, removes, or renames a field | Yes | The former plan proposed zero values plus a named one-way migration for renames; the implemented policy intentionally does not add a legacy conversion path. Invalid or unknown content resets the affected file. |

A silent mutating fixer is the wrong owner for every row. For the second threat
it destroys the operator's edit and reports nothing but a log line; today it
does not even write the defaults it claims to. For the third it hand-maintains
an allow-list of keys that a prefilled decode handles for free, for every
present and future field.

## Historical defect evidence

The C1-C9 findings below were confirmed against the pre-simplification
implementation. They are retained as rationale and regression context, not as
descriptions of live current behavior.

Evidence classes are stated per defect. Source-verified means read from the
pre-simplification tree at the time of analysis. Reproduced means observed by
running the real package against `goccy/go-yaml v1.19.2` in a scratch harness
outside the repository. No
repository Make target was run to produce this document.

### Live — reachable on a normal install

**C1 — `repairDockerFolderPaths` never converges.** `validator.go:348-361`
drops a configured folder that exists as a regular file, finds `validFolders`
empty, restores `defaults.Docker.Folders` — which is `<base>/docker`
(`settings.go:96-101`), the path just rejected — and returns `true`
unconditionally. The bridge therefore logs the warning and atomically rewrites
the config on every `OpenUserStore`, forever, and the offending folder is never
actually removed. Reproduced: four consecutive `repairConfig` calls produce four
inode changes and zero content changes.

**C2 — `showHiddenFiles` is never backfilled.** `repairMissingAppSettings`
(`validator.go:171-208`) probes six keys and this is not one of them.
`ShowHiddenFiles` is the only app setting with a non-zero default
(`settings.go:11`) that neither that list nor `repairInvalidConfigValues`
rescues, and `types.go:92` carries no `omitempty`, so `false` is written
explicitly and persists. `ConfigProvider.tsx` merges with `??`, which does not
fall back on `false`. Reproduced from a config containing only
`appSettings.theme`. Self-heals on the user's first toggle.

**C3 — `chunkSizeMB: 0` and `viewModes: {}` are unrepresentable on disk.**
`types.go:100-101` documents `0 = use default`, `apply_app.go` accepts 0,
`omitempty` then drops the key on write, and `validator.go:204-207` backfills
it to 1 on the next start. `viewModes` has the same shape: a payload of `{}`
clears it, and `validator.go:200-203` re-expands all twelve entries. The user's
choice is silently reverted at restart. Source-verified.

### Latent — requires an externally modified file

**C4 — the "rewrite defaults" recovery writes defaults, then clobbers them.**
`validator.go:43-48`: when both the strict and permissive decodes fail, the
function writes full defaults and returns `(false, nil)`. Because the error is
nil, `repairConfig` does not stop at `validator.go:26-28`. It continues with the
half-decoded `cfg`, `repairInvalidConfigValues` always fires (`Theme` and
`PrimaryColor` are `""`, so `changed` is always true), and `validator.go:35`
rewrites the file from that struct — overwriting the defaults just written. The
log line says "rewriting defaults"; that is not what ends up on disk.

The single-bad-scalar case is worse than the syntax-error case. A file
containing `theme: PURPLE`, an invalid `themeColors` entry, or a wrong-typed
integer is valid YAML, so goccy zeroes only the mapping containing the offending
key and decodes its siblings normally. Reproduced: a fully customised config
lost `showHiddenFiles`, `sidebarCollapsed`, `navigationMode`, `dockTileColors`,
`hiddenCards`, `layoutOrders`, `terminalFontSize`, `chunkSizeMB`, `themeColors`,
and `dockAccentGradient`, while `docker` and `dismissals` survived from the
file — proving the config was never reset to defaults. The next start restores
only what the probe list covers; `showHiddenFiles: false` and gradient `{0,0}`
are permanent, because `{0,0}` passes `ValidateDockAccentGradient`
(`types.go:170-188`) and the key has no `omitempty` so it stays "present".

**C5 — `ValidateConfig` is not a superset of the decoder.** It never applies
`filepath.IsAbs` (`validator.go:413-431`) while `types.go:243-256` rejects
relative paths, and it validates 6 of the 18 `*CSSColor` fields
(`validator.go:303-310` versus `types.go:54-71`) while `types.go:227-229`
rejects `""` on all of them. Since `ValidateConfig` is the only gate on
`UserStore.Update` (`store.go:150`), the store can persist a file that no later
read can parse: every subsequent `Update` then fails at `store.go:141` for the
life of the process. Not reachable from any current caller, because the handler
layer pre-validates. Source-verified plus reproduced through the real store.

**C6 — startup repair writes without the lock `Update` uses.** `store.go:137`
serialises updates through a cross-process flock, and `store.go:16-20` documents
that invariant. `init.go:53` to `validator.go:18,35` is an unlocked
read-modify-write of the same file. `SingleSessionPerUser` defaults to `false`
(`common/session/session.go:54`), so every login spawns its own bridge and a new
session's repair can discard a concurrent session's committed change. The
window is zero while repair does not write, and permanent under C1.
Source-verified.

**C7 — `CSSColor.UnmarshalYAML` rejects a value the type documents as legal.**
`types.go:221-232` rejects `""`; `types.go:74-78` states that empty colors
derive from the active theme accent, and `types.go:172-176` explicitly permits
empty. Safe only because `omitempty` never writes the key. A file containing
`startColor: ""` takes the C4 path. Source-verified.

**C8 — `Update` reports failure after the write has committed.**
`store.go:156-163`: `writeConfigFrom` succeeds, then an `ensureFilePerms`
failure returns an error and skips the `s.cfg` refresh, leaving disk updated and
the in-memory snapshot stale for the process lifetime. `ensureFilePerms` is also
redundant here — `utils.WriteFileAtomic` already applies the mode.
Source-verified.

**C9 — the permissive decode error is discarded.** `validator.go:44-47` logs the
*strict* error, so for a file with both an unknown key and an invalid value the
operator is told about the unknown key while the invalid value is what actually
forced the destructive path. Source-verified.

### Historical test floor

No test covers any of C1–C9. `settings_test.go` writes only valid, known-key
YAML and pins only the missing-key backfill and the legacy layout migration;
`store_test.go` always goes through `writeConfigFrom`, and its only
`ValidateConfig` coverage is one invalid `primaryColor`. `logYAMLError`
(`validator.go:367-381`) is never executed.

## Format decision

**Keep YAML. There is no JSON migration.** The two per-user files remain YAML,
and the bridge does not add a database or a YAML-to-JSON conversion shim.

Dropping YAML here removes no dependency: `goccy/go-yaml` stays a direct require
for three foreign formats that can never be JSON — `handlers/docker/compose.go`,
`handlers/network/internal/network/backend_netplan.go`, and
`handlers/virt/cloud_init.go`. It also removes no complexity: `yaml.Strict()`
sets exactly one flag with a direct equivalent in
`jsonv2.RejectUnknownMembers(true)`, which is already the repository idiom in
three places; the three `UnmarshalYAML` bodies are line-for-line identical as
`UnmarshalJSON`; and the presence-probing this plan deletes is format-agnostic.
Counting a migration shim and its tests, the swap is net **+50 to +70 lines**.

The historical proposal to switch formats was declined. YAML remains useful
for operator inspection and is already a direct dependency for Compose,
netplan, and cloud-init. The old consistency argument for JSON does not
authorize a migration, and an absent sidecar does not trigger one: each YAML
file follows the independent create/load/reset rules above.

## Principles

- One mechanism per file: strict decode, pure validation, and an atomic reset
  on content failure.
- Functional and persisted UI defaults are both established by the backend. An
  empty or sparse UI document is expanded to a complete effective value before
  it crosses the API boundary.
- Theme design tokens and natural component layout stay at the use site; they
  are presentation implementation, not a second configuration default table.
  `EffectiveJobSettings` likewise remains the runtime owner of job integers.
- A rejected document is reported and replaced, never permissively salvaged or
  silently rewritten into a third state.
- Docker-folder validation is structural; it does not inspect, create, remove,
  or rewrite filesystem paths.
- Operational failures (I/O, permissions, locking, or security checks) fail
  the operation and do not replace the existing document.

## Historical implementation plan (superseded)

The phase sections below are retained as an audit trail for the old
repair-based proposal. They are complete or superseded by the current policy
above; they are not a remaining TODO, and their migration/repair
recommendations are not normative.

### Phase 1 — correctness and test floor

These land together. The convergence test cannot pass without the C1 fix, and
no later phase is safe to land without this test floor.

1. Fix **C4**. Have `parseAndSanitizeConfig` report that it already wrote
   defaults — a third return value or a sentinel error — and have `repairConfig`
   return immediately, setting `*cfg = *DefaultSettings(base)` so nothing
   downstream can rewrite from the half-decoded struct.
2. Fix **C1**. Return the real `changed` value, or skip the defaults fallback
   when the default path is itself the rejected entry.
3. Add the tests that pin what must survive every later phase:
   - unparseable bytes: file equals `DefaultSettings(base)` and round-trips;
   - unknown key: key gone, every known value preserved;
   - `theme: PURPLE`: theme reset, **every sibling value preserved**;
   - `navigationMode: bogus` and `dockTileColors: bogus`: reset;
   - gradient `{90,10}` on disk: reset;
   - `folders: [/, /a, /a]`: repaired to a single valid entry;
   - a folder that exists as a regular file: dropped once, and **converges** —
     a second `repairConfig` writes nothing.

### Phase 2 — prefill defaults, delete the probing

Set `cfg := *DefaultSettings(base)` before the decode in `repairConfig`, then
delete `repairMissingDefaultValues`, `repairMissingAppSettings`,
`repairMissingDockerSettings`, `repairMissingJobSettings`, `childMap`, and
`hasMapKey` (`validator.go:103-135` and `171-268`, 131 lines).

This covers every present and future field, nested structs included, with no
allow-list to maintain, and it fixes **C2** and **C3** as a consequence: an
explicitly persisted `false`, `0`, or `{}` is honoured rather than overwritten.

The apparent staleness of the current probe list is misleading and should not be
"fixed" by extending it. There are no probes for `navigationMode`,
`dockTileColors`, `layoutOrders`, `hiddenCards`, `terminalFontSize`,
`docker.proxy`, or `dismissals`, but every one of those defaults to the Go zero
value and carries `omitempty`, so a probe would be a no-op. The real gap is
exactly one field, `showHiddenFiles`, and prefilling closes it.

### Phase 3 — delete unreachable validation

1. Delete `validateThemeColorMode` and `themeColorsNeedReset`
   (`validator.go:296-342`, 45 lines) and their call sites at
   `validator.go:83-86` and `407-410`. They are two hand-listed copies of the
   same six pointers, they cover 6 of 18 fields, and `types.go:221-232`
   guarantees any decoded `*CSSColor` is already valid.
2. Delete the job repair and job validation (`validator.go:91-99`, `224-251`,
   `433-450`, roughly 55 lines) and leave `EffectiveJobSettings`
   (`settings.go:114-135`) as the sole owner. It is strictly stronger (`<= 0`
   versus `< 0`) and runs on every read.
3. Delete the `requireMountsForFolders` probe (`validator.go:217-220`). It
   assigns `false` over `false` and returns `changed = true`, forcing a rewrite.
4. Keep a single approximately 40-line `sanitize(cfg, defaults)` covering only
   the five reachable invalid states listed in
   [historical baseline](#historical-baseline-pre-simplification).
5. Widen `ValidateConfig` into a genuine superset of the decoder to close
   **C5**: apply `filepath.IsAbs` per folder, and validate all 18 colors from
   one shared field list also used by `apply_app.go`. Accept `""` in
   `CSSColor.UnmarshalYAML` to close **C7**.
6. Close **C6** by wrapping the startup read-modify-write in
   `withExclusiveConfigLock`, **C8** by assigning `updated` before
   `ensureFilePerms` or dropping the redundant call, and **C9** by logging the
   permissive error and rewording the warning to match what the code does.

### Phase 4 — remove the generator apparatus

Delete `generator.go`, `config_generated.yaml`, the `//go:generate` line at
`init.go:1`, the `generate` prerequisite in the `Makefile`, and its help line
(175 lines plus a build-time `git` dependency).

The filename appears exactly once in the repository — in its own writer at
`generator.go:61`. There is no `//go:embed`, no test, no CI drift check, and no
documentation reference. Its content is also wrong: `generator.go:67` feeds
`version.DataDir`, so the artefact advertises
`folders: [/var/lib/linuxio/docker]` while the real runtime default is
`<homedir>/docker` (`init.go:40`, `settings.go:99`) — and that wrong value has
already been copied into `ConfigProvider.tsx:155` and `useConfig.test.tsx`.
Each regeneration additionally commits a wall-clock timestamp and the
generating developer's git branch name.

If the artefact is kept instead, fix the base directory and drop the
timestamp/branch header before adding any drift gate.

Separately, and outside this package: `frontend/src/api/generated/*` does need a
drift gate. CI never runs `make generate`, only `build`, `build-nocheck`, and
`fastbuild` depend on it, and `make tsc-ci` type-checks the stale file against
itself. Track that as repository tooling, not as config work.

### Phase 5 — type and API surface reduction

1. Collapse the 18 `*CSSColor` fields in `ThemeColors` to value `CSSColor` with
   `omitempty`. The pointer exists to distinguish unset from empty, but
   `types.go:227` makes empty undecodable, so the value form is byte-identical
   on disk — `DockAccentGradient` (`types.go:77-78`) already does exactly this,
   so the package currently disagrees with itself. `cloneThemeColors` and
   `cloneCSSColor` fold into the struct copy, and `apply_app.go`'s `**CSSColor`
   table becomes `*CSSColor` (about 45 lines across three packages).
2. In `UserStore.Update`, mutate `current` directly and clone once instead of
   four times.
3. Delete `filepathJoinClean` (`filepath.Join` already cleans) and
   `guardConfigPath` (the fourth of five symlink refusals on the same path, and
   it runs after `CheckConfig` has already done it in the same call chain).
4. Unexport `Initialize`, `Homedir`, `CheckConfig`, `ValidateConfig`,
   `DefaultDocker`, `IsValidNavigationMode`, and the four `DockTileColors*`
   constants — none has a consumer outside the package. Delete
   `PersistedTheme.String` and `AbsolutePath.String`, which have no callers
   anywhere.
5. Drop the `username` parameter and the mismatch guard from `SnapshotForUser`
   and `UpdateForUser` (`store.go:75-79`, `95-97`). A bridge process serves one
   session and one store (`cmd/root.go:84-92`), and every caller derives the
   username from that same session, so the check is a tautology threaded through
   six handler signatures.
6. Replace the prefix tests in `IsValidCSSColor` (`colors.go:53-57`) with a real
   predicate. Five bare `HasPrefix` checks with no closing-paren or argument
   validation mean `var(--x); } * {…` currently validates. This is a correctness
   defect, not a security hole: the only path from a config color to the DOM is
   `root.style.setProperty` and React inline `style`, both CSSOM-parsed.
7. Document the two tag families above `type Settings`: the `yaml` tags are the
   disk schema, and the `json` tags mirror `apischema.AppConfig` for the parity
   guard in `handlers/config/contracts_test.go`. Mark `types.go:98-99` as
   disk-only migration inputs. Add a package doc comment stating that the file
   is machine-owned and rewritten in full.

Expected result across Phases 2–5: `validator.go` 453 to about 145,
`store_clone.go` 98 to about 58, `colors.go` 60 to about 35, `utils.go` 170 to
about 152, with `types.go`, `store.go`, and `settings.go` roughly unchanged —
about 1,470 compiled lines down to about 1,070, plus `generator.go` (86) and
`config_generated.yaml` (89) deleted.

### Phase 6 — optional format migration (declined)

The historical proposal to target `encoding/json/v2` with
`RejectUnknownMembers(true)` and `omitzero`, including a read-old/write-new
shim, was declined. YAML is retained and no format migration is planned.

## Resolved ownership question

- The `apischema` and generated frontend contract changed because a separate UI
  resource is the ownership boundary. The persisted structs remain distinct
  from the wire shapes (`handlers/config/contracts.go`).
- Default duplication is resolved at the bridge boundary: backend defaults own
both functional settings and persisted presentation preferences. The frontend
renders the effective `config.get_ui` response and writes complete snapshots;
it does not maintain a second persisted-default table.

## Still out of scope

- `docker.proxy.baseDomain` and `tlsEmail`, which the Caddyfile generator
  consumes but no frontend code writes, and the `jobs` branch of the wire
  contract, which has no frontend reader or writer. Both are product gaps.

## Historical recommendations (superseded)

The following "Do not do" rules belonged to the former repair/migration plan.
They are preserved to explain the original risk analysis, but they must not be
read as current behavior. Under the implemented policy, content failures reset
the affected YAML file, there is no legacy conversion, and Docker-folder
filesystem usability is never persisted or mutated.

- **Do not delete the repair layer outright.** Four of its responsibilities must
  survive in some form: the self-healing parse, without which a single
  unparseable config makes `OpenUserStore` fail at `cmd/root.go:84` and the
  bridge session does not start; unknown-key stripping, without which removing
  any field bricks a downgraded session; the legacy
  `dashboardOrder`/`containerOrder` migration (`validator.go:141-169`), which
  has no completion marker so its removal can never be proven free; and
  dropping a docker folder that exists as a regular file, which nothing else
  does and which `compose.go` would otherwise fail on with a worse error.
- **Do not reuse the `json` tags as the disk schema.** `types.go:98-99` are
  `json:"-"`, so the legacy layout migration would become unreachable and would
  silently discard unmigrated layouts.
- **Do not strip the `json` tags as unused metadata.** They never reach the
  wire — `contracts.go` converts field by field from `handlers.go:33` — but
  they are the parity specification enforced by `contracts_test.go`. Removing
  them compiles, ships, and breaks exactly one test.
- **Do not delete `DashboardOrder`/`ContainerOrder` piecemeal.** The
  strict-parse fallback swallows the now-unknown keys and hides the data loss.
  Delete the fields, the migration, and its tests in one commit, or not at all.
- **Do not justify a JSON migration as dependency reduction.** `goccy/go-yaml`
  stays for Compose, netplan, and cloud-init.
- **Do not switch to `encoding/json` v1.** Struct `omitempty` does not omit
  structs, so `"proxy":{"caddyEnabled":false}` would start appearing in every
  install's file where goccy omits the key entirely.
- **Do not convert only the fields that look like they need pointer defaults.**
  `showHiddenFiles` must be covered or C2 returns, and the six job integers must
  not be, since `EffectiveJobSettings` already owns them.
- **Do not reset `docker.folders` during any migration or repair shortcut.** It
  determines which Compose stacks are visible, and it is the one setting a user
  cannot re-enter in seconds.

## Historical verification notes

The old phase plan expected backend or repository-wide Make targets. Those
notes describe the historical plan; they do not reopen the declined migration
or define the current ownership contract. Current changes should use the
repository's applicable verification target and inspect any tooling-generated
worktree changes afterward.
