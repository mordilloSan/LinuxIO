# Jobs review — status and handoff

**Started:** 2026-07-30. **Updated:** 2026-07-30. **Branch:** `dev/v0.17.0`.
**Companion docs:** `docs/jobs-architecture-migration-plan.md` (the strategy), `CODE_REVIEW_FINDINGS.md` (the prior working-tree review).

---

## The original goal

> "right now every mutation is a job. Full review of jobs code. I want to keep react query mutations clean."

That goal is now complete for the existing handler-form routes. The migration
moved all 106 progressless handlers to the normal Query/`useAction` path; the
preparation and defect cleanup that made the migration safe are recorded below.

### What the review found and the migration left

The jobs framework is now on a sounder footing — `jobs.go` / `router.go` are careful, well-tested concurrency code, including the physical-execution admission fix described below. The review's remaining problem was **scope**: it was applied to ~107 routes that needed one round trip and a plain React Query mutation.

The codebase had already drawn the correct line without knowing it. The final
route inventory now reflects that line:

| Declaration | Count | Emits progress? | In the frontend recovery whitelist? |
| --- | ---: | --- | --- |
| `apischema.Runner[…].Run(…)` | 19 | all | **15 of 19 — exactly the whitelist** |
| `apischema.Job[…].HandleEvents(…)` | 2 | both | **0** |
| `apischema.Query` | 203 | — | — |
| `apischema.DuplexRoute` | 6 | — | — |

The 15 `Runner` routes (`filebrowser.compress/extract/copy_batch/move_batch/index/upload/upload_batch/download/archive/chmod_batch/delete_batch`, `docker.compose`, `packages.update`, `storage.run_smart_test`, `system.install_capability`) are **identical** to the 15 types in `frontend/src/constants/backgroundJobTypes.ts` and the `useRecoveredJobs` switch. The other 4 Runners are `NoEndpoint()` log-follow / app-update streams.

Only 2 handler files emit progress at all: `filebrowser/filebrowser.go` and `virt/handlers.go`.

**Real jobs are now the 21 job-mode routes.** The frontend has one production
`useJobAction` call site (`filebrowser.resource_patch`) and seven
`useJobStreamAction` call sites; bounded commands use `useAction`.

### What "every mutation is a job" cost

1. **Up to 3 wire ops per mutation instead of 1** — route request (server holds ≤25 ms hoping the job settles, `router.go` `InitialJobSettleTimeout`) → `jobs.attach` (a new yamux stream) → `jobs.get`. Anything that shelled out missed the 25 ms window, so this was the normal path.
2. **`ActionDefault` silently serialized every mutation.** `.Handle()` took no policy, so every handler-form job route got `MaxActivePerOwnerRoute: 1`, `QueueLimit: 16`, `Timeout: 120min`. One container start at a time per user; the 18th concurrent → `429 job queue full`. Nobody chose that for `accounts.lock_user`.
3. **Two possible owners per job still require progress/feedback de-duplication** on the frontend. Query invalidation is deliberately simpler: every mapped terminal job event invalidates, even when a local mutation also does so, because a harmless duplicate is safer than stale data after detachment. The remaining ref-sets in `useBackgroundJobRuntime.ts` and the `terminalJobFeedback` registry are confined to the 21 real job routes.
4. **~3,240 non-test frontend lines** in `hooks/backgroundJobs/` + contexts + `api/jobs.ts`, serving 8 call sites.

---

## Status

### Done

**1. `jobs.cancel` was declared `Job`; now `Query`.** (`backend/bridge/handlers/jobs/routes.go`)

It returns the *target* job's snapshot, so `useJobAction` duck-typed that snapshot as a freshly started job and fed it to `waitForJobCompletion` — which threw 499 for an already-canceled job, or opened a *second* attach stream for a still-running one and marked it locally handled. Every page-level cancel silently rejected. Guarded by `TestHandleEventsInventoryIsCurrent`'s sibling in `frontend/src/api/react-query.test.ts` ("keeps jobs.cancel out of job mode…"), which is mutation-tested.

Note: `assertRouteMode` catches wrong-hook usage at **render time**, not compile time. `CommandEndpoint` exposes every hook regardless of mode. Real compile-time separation needs mode-specific generated endpoint types (plan Stage 1).

**2. Typed handler binding forms.** (`backend/bridge/apischema/schema.go`)

| Form | Shape | Count |
| --- | --- | ---: |
| `Handle` | `func(ctx, Req) (Result, error)` — **Result is compile-checked** | 138 |
| `HandleVoid` | `func(ctx, Req) error` — panics at bind time if Result isn't `NoResponse` | 61 |
| `HandleEvents` | `func(ctx, Req, emit) error` — raw emitter, the old shape | 3 |

`EmitResult` call sites went **205 → 3**. Before this, `Events.Result` was `any` and the declared `Result` type param was **never** checked against handler output.

`NoResponse` must stay off the wire as `nil` — it generates TypeScript `void`, so emitting the zero struct would send `{}` to a `void` consumer and stop job snapshots from omitting `result`. Both binding forms enforce this; `TestNoResponseRoutesEmitNilNotZeroStruct` is mutation-tested.

**3. `HandleWithPolicy` deleted** (0 call sites), along with the now-orphaned `HandlerBinding.Policy` field it was the only setter for. `AttachHandler` now passes `bridgeipc.ActionDefault` explicitly, so the fact that *every* handler-form job route runs under `ActionDefault` is stated rather than implied.

**4. Void-ness as shared route metadata** (`apischema.IsVoidType` / `IsEmptyRequestType` / `TypeSpec.Void()` in `contracts.go`). The emit path and the TypeScript generator previously each had their own `NoResponse` comparison; they now share one. Costs no extra reflection — `RouteSpec.Result` is already a `reflect.Type`. `schema.go` no longer imports `reflect`. The generator's `sameType` (which deref'd the wrong operand) was deleted.

**5. ~78 routes moved off the raw emitter** across wireguard, docker, storage, system, filebrowser, config, packages, monitoring, systemd, network, accounts, datetime, control, hostname, terminal, indexer, logs, virt, shares, power.

**6. B7 admission accounting now follows physical handler execution.** A canceled or timed-out job may publish its terminal state promptly, but its route/owner slot is not released until the underlying handler goroutine actually exits. Queued successors therefore cannot push real execution above `MaxActivePerRoute` or `MaxActivePerOwnerRoute`, even when a handler ignores cancellation. Tests cover cancel, timeout, and the direct `Timeout=0` path.

**7. Public job snapshots no longer contain decoded requests.** `request` was removed from the bridge snapshot, apischema model, generated TypeScript, and frontend consumers. The request remains private execution state and is cleared from the registry-held job on queued cancellation or any terminal transition. Exactly 15 recoverable Runner routes opt into a fixed `JobMetadata` projection; credential-bearing handler jobs expose no metadata. Transport-level tests cover start, get/list/cancel, `jobs.events` initial/live frames, and attach replay/terminal frames with a sentinel secret.

**8. Contract drift went from 32 routes to 3.** Apischema is authoritative for the duplicate models, progressless `any` domains now return their declared types, Docker preserves its extra SDK data in typed contracts and surfaces it in the UI, and filebrowser search/subfolder responses are canonicalized. The three intentional/deferred routes are listed below.

### Bugs fixed en route

**`wireguard.add_interface` leaked a private key into a job snapshot.** `AddInterface` returned `map[string]any{… "private_key": privKey.String() …}` on a route declaring `NoResponse` (generated TS: `void`, read by nobody). Because it is a `Job` route, that key sat in the snapshot for `DefaultTerminalJobTTL` (30 min), readable via `jobs.get` / `jobs.list` / `jobs.events` by any session of the same user. Now bound `HandleVoid`, so it emits `nil`.

**Two false drift annotations.** `docker.check_container_update` and `docker.update_container` matched their declared types all along; a mechanical pass had skipped them for handler-body shape and I annotated them as drifted. This is why the prose comments were replaced with a checked table (below).

### Verification (all green)

```
make generate     # regenerate route metadata after schema changes
make test         # cross-stack audit: backend clean; 99 frontend files / 552 tests
git diff --check  # clean
```

**`make generate` leaves the generated TS unformatted.** The committed files are post-`oxfmt`, so a bare `make generate` produces ~700 lines of formatting churn. Always follow it with:
```
make test
```
The frontend lint phase formats the generated files. Folding that formatting
into the `generate` target would remove the trap.

---

## Remaining contract drift — 3 routes

The authority is **`handleEventsInventory`** in `backend/bridge/handlers/handler_pattern_test.go`. It is a checked table, not prose: `TestHandleEventsInventoryIsCurrent` fails if a route is bound with `HandleEvents` and missing from it, if an entry is no longer bound that way, or if the counts disagree. **Do not describe drift in per-file comments** — that is what this replaced, and two of those comments had already gone stale.

| Category | Count | Meaning |
| --- | ---: | --- |
| `map-vs-struct` | 1 | `systemd.get_unit_info` — `map[string]any` against a declared struct |
| `progress-emitter` | 2 | `filebrowser.resource_patch`, `virt.create` — **legitimate and permanent** |

Verify the tally against the source of truth rather than trusting this table:
```bash
sed -n '/handleEventsInventory = map/,/^}/p' backend/bridge/handlers/handler_pattern_test.go \
  | grep -oE "mapVsStruct|progressEmitter" | sort | uniq -c
```

`systemd.get_unit_info` remains wire-correct and intentionally deferred: converting the dynamic D-Bus map still has the worst effort-to-value ratio, especially where D-Bus numeric types do not match the public struct exactly.

### Decisions applied

**D1 — Apischema is authoritative for the 18 duplicate-model routes.** Local domain values are converted at the boundary where needed. Ordinary optional strings are scalar `string` with `omitempty`; pointers remain only where absence must stay distinguishable from a valid zero, such as UID/GID `0`, password-aging counters, or known-zero WireGuard runtime statistics.

**D2 — `docker.list_volumes` preserves the SDK extras.** The typed contract includes `Status`, `UsageData` (`RefCount`/`Size`, including `0` and `-1`), and optional `ClusterVolume`; the table/card/expanded UI uses them. The list still uses only `VolumeList` on its existing 10-second poll. It does not call Docker disk usage to fill missing size/reference data.

**D3 — `docker.list_networks` also preserves the SDK extras.** The typed contract and UI include `Created`, `Attachable`, `Ingress`, `ConfigOnly`, container `EndpointID`, and the full previously exposed IPAM shape (`Driver`, `Options`, `IPRange`, and auxiliary addresses).

**D4 — `systemd.get_unit_info` is deferred.** No behavior or contract change was made.

**D5 — `filebrowser.subfolders` drops `bytes`.** The private decoder and public typed response use only `path`, `name`, `size`, and `mod_time`; the old `bytes` fallback is gone.

**D6 — `filebrowser.search` emits only `mod_time`.** That is the field defined by the upstream indexer's [`EntryResult`](https://github.com/mordilloSan/indexer/blob/main/storage/queries.go#L54-L64). A private compatibility decoder accepts `modTime` and `modified` with precedence `mod_time` → `modTime` → `modified`, but aliases never reach apischema, generated TypeScript, or the frontend. The typed result also preserves `inode` and optional `total_size` / `total_files` / `total_dirs`.

---

## Direct-action migration

**Completed:** all 106 progressless handler-form `Job` routes now use the
normal Query/`useAction` request-response path. The only handler-form jobs
left are the two progress emitters, `filebrowser.resource_patch` and
`virt.create`; the 19 `Runner` routes are unchanged. Generated route modes are
now **203 Query, 21 Job, and 6 Duplex**, enforced by an apischema test.

The two prerequisite findings that could not wait for the flip are already closed:

- **Job-snapshot secrets:** raw requests were removed at the snapshot boundary for every route, including `virt.create`; this no longer depends on moving credential routes to Query.
- **B7:** admission slots remain occupied until physical handler exit, independently of the job's public terminal state.

### Self-severing action decision

`control.reboot`, `control.power_off`, `control.logoff`, and all six network
apply routes now use direct actions too. Their handlers complete native service
acceptance before connection loss can occur; the UI must treat a lost response
as an expected ambiguous outcome and must not retry it automatically. The route
declarations carry this decision until the plan's explicit
`native_handoff`/`expected_loss` manifest fields are introduced.

Two properties that make this safer than it looks:

- **It is self-policing.** `assertRouteMode` throws at render if a route's mode and hook disagree, so a missed call site is an immediate visible error, never silent breakage — provided backend, frontend and `make generate` land together per batch.
- **Query mode loses no detachment property.** The dispatch `ctx` is already bridge-scoped, not stream-scoped (`backend/bridge/cmd/yamux.go`), so a Query handler already survives browser stream reset exactly as a job does.

### Deliberately not doing

- Touching the transport. WebSocket + yamux is measured and fine.
- Starting the durable-jobs core (plan Stages 9–14). Durability beyond the bridge is *new* scope, not a restoration, and is not on the path to "React Query mutations stay clean."
- Adding `VoidQuery`/`VoidJob` constructors now. `HandleVoid` already enforces
  the binding boundary; mode-specific generated endpoint types and any
  additional constructor vocabulary remain Stage 1 contract work.

---

## Orientation commands

```bash
# binding-form counts
for f in ".Handle(" ".HandleVoid(" ".HandleEvents("; do printf "%-16s " "$f"; \
  find backend/bridge/handlers -name '*.go' ! -name '*_test.go' -exec grep -oh -- "$f" {} \; | wc -l; done

# the drift inventory (authoritative)
sed -n '/handleEventsInventory = map/,/^}/p' backend/bridge/handlers/handler_pattern_test.go

# route modes
grep -oE '": "(job|query|duplex)"' frontend/src/api/generated/route-metadata.ts | sort | uniq -c

# frontend job-hook call sites
grep -rno "\.useJobAction(\|\.useJobStreamAction" frontend/src/ --include=*.ts --include=*.tsx \
  | grep -v "\.test\." | grep -v "^frontend/src/api/"

# full check
make test
```

### Stale references to be aware of

`docs/jobs-architecture-migration-plan.md` lines 20 and 61 still cite `HandleWithPolicy` (deleted) and list the old `Handle/HandleWithPolicy/Run/Duplex` vocabulary. The plan's diagnosis and its `Query/Action/Duplex/FileTransfer/DurableJob` contract set remain sound; only those two lines describe a tree that no longer exists.
