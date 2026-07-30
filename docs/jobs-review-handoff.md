# Jobs review — status and handoff

**Started:** 2026-07-30. **Branch:** `dev/v0.17.0`.
**Companion docs:** `docs/jobs-architecture-migration-plan.md` (the strategy), `CODE_REVIEW_FINDINGS.md` (the prior working-tree review).

---

## The original goal

> "right now every mutation is a job. Full review of jobs code. I want to keep react query mutations clean."

That is still the goal, and **the main body of it has not started.** Everything completed so far is preparation and defect cleanup that the preparation exposed. Read the next two sections before picking a task.

### What the review found

The jobs framework itself is sound — `jobs.go` / `router.go` are careful, well-tested concurrency code. The problem is **scope**: it is applied to ~107 routes that need one round trip and a plain React Query mutation.

The codebase had already drawn the correct line without knowing it:

| Declaration | Count | Emits progress? | In the frontend recovery whitelist? |
| --- | ---: | --- | --- |
| `apischema.Runner[…].Run(…)` | 19 | all | **15 of 19 — exactly the whitelist** |
| `apischema.Job[…].Handle(…)` | 108 | 2 | **0** |
| `apischema.Query` | 97 | — | — |
| `apischema.DuplexRoute` | 6 | — | — |

The 15 `Runner` routes (`filebrowser.compress/extract/copy_batch/move_batch/index/upload/upload_batch/download/archive/chmod_batch/delete_batch`, `docker.compose`, `packages.update`, `storage.run_smart_test`, `system.install_capability`) are **identical** to the 15 types in `frontend/src/constants/backgroundJobTypes.ts` and the `useRecoveredJobs` switch. The other 4 Runners are `NoEndpoint()` log-follow / app-update streams.

Only 2 handler files emit progress at all: `filebrowser/filebrowser.go` and `virt/handlers.go`.

**Real jobs ≈ 21. The other ~107 are mutations.** The frontend agrees: **8** `useJobStreamAction` call sites vs **117** `useJobAction`.

### What "every mutation is a job" costs

1. **Up to 3 wire ops per mutation instead of 1** — route request (server holds ≤25 ms hoping the job settles, `router.go` `InitialJobSettleTimeout`) → `jobs.attach` (a new yamux stream) → `jobs.get`. Anything that shells out misses the 25 ms window, so this is the normal path.
2. **`ActionDefault` silently serializes every mutation.** `.Handle()` takes no policy, so every handler-form job route gets `MaxActivePerOwnerRoute: 1`, `QueueLimit: 16`, `Timeout: 120min`. One container start at a time per user; the 18th concurrent → `429 job queue full`. Nobody chose that for `accounts.lock_user`.
3. **Two possible owners per job → four dedup mechanisms** on the frontend: `locallyHandledJobIds` + 5 s retention (`api/jobs.ts`), five ref-sets in `useBackgroundJobRuntime.ts`, the `terminalJobFeedback` registry, and `useTerminalFeedbackOwnership`. This is the machinery behind the whole B1/B2/D5/D6 cluster in `CODE_REVIEW_FINDINGS.md`. Restrict jobs to ~21 routes and the ambiguity shrinks to those.
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
| `Handle` | `func(ctx, Req) (Result, error)` — **Result is compile-checked** | 112 |
| `HandleVoid` | `func(ctx, Req) error` — panics at bind time if Result isn't `NoResponse` | 58 |
| `HandleEvents` | `func(ctx, Req, emit) error` — raw emitter, the old shape | 32 |

`EmitResult` call sites went **205 → 32**. Before this, `Events.Result` was `any` and the declared `Result` type param was **never** checked against handler output.

`NoResponse` must stay off the wire as `nil` — it generates TypeScript `void`, so emitting the zero struct would send `{}` to a `void` consumer and stop job snapshots from omitting `result`. Both binding forms enforce this; `TestNoResponseRoutesEmitNilNotZeroStruct` is mutation-tested.

**3. `HandleWithPolicy` deleted** (0 call sites), along with the now-orphaned `HandlerBinding.Policy` field it was the only setter for. `AttachHandler` now passes `bridgeipc.ActionDefault` explicitly, so the fact that *every* handler-form job route runs under `ActionDefault` is stated rather than implied.

**4. Void-ness as shared route metadata** (`apischema.IsVoidType` / `IsEmptyRequestType` / `TypeSpec.Void()` in `contracts.go`). The emit path and the TypeScript generator previously each had their own `NoResponse` comparison; they now share one. Costs no extra reflection — `RouteSpec.Result` is already a `reflect.Type`. `schema.go` no longer imports `reflect`. The generator's `sameType` (which deref'd the wrong operand) was deleted.

**5. ~78 routes moved off the raw emitter** across wireguard, docker, storage, system, filebrowser, config, packages, monitoring, systemd, network, accounts, datetime, control, hostname, terminal, indexer, logs, virt, shares, power.

### Two bugs fixed en route

**`wireguard.add_interface` leaked a private key into a job snapshot.** `AddInterface` returned `map[string]any{… "private_key": privKey.String() …}` on a route declaring `NoResponse` (generated TS: `void`, read by nobody). Because it is a `Job` route, that key sat in the snapshot for `DefaultTerminalJobTTL` (30 min), readable via `jobs.get` / `jobs.list` / `jobs.events` by any session of the same user. Now bound `HandleVoid`, so it emits `nil`.

**Two false drift annotations.** `docker.check_container_update` and `docker.update_container` matched their declared types all along; a mechanical pass had skipped them for handler-body shape and I annotated them as drifted. This is why the prose comments were replaced with a checked table (below).

### Verification (all green)

```
cd backend && go build ./...          # clean
gofmt -l ./bridge/ ./common/           # clean
make golint-only                       # 0 issues
go test ./... -count=1                 # 42 packages ok
make generate                          # generated API byte-identical throughout
```

One **pre-existing** failure: `TestValidateComposeFileAllowsPiHoleDNSProtocols`. Confirmed failing at clean backend HEAD — unrelated to this work.

**`make generate` leaves the generated TS unformatted.** The committed files are post-`oxfmt`, so a bare `make generate` produces ~700 lines of formatting churn. Always follow it with:
```
cd frontend && npx oxfmt -c config/.oxfmtrc.json src/api/generated/*.ts
```
Folding that into the `generate` target would remove the trap.

---

## Remaining contract drift — 32 routes

The authority is **`handleEventsInventory`** in `backend/bridge/handlers/handler_pattern_test.go`. It is a checked table, not prose: `TestHandleEventsInventoryIsCurrent` fails if a route is bound with `HandleEvents` and missing from it, if an entry is no longer bound that way, or if the counts disagree. **Do not describe drift in per-file comments** — that is what this replaced, and two of those comments had already gone stale.

| Category | Count | Meaning |
| --- | ---: | --- |
| `duplicate-model` | 18 | A handler package keeps its own copy of an apischema model and returns that |
| `any-domain` | 11 | The domain function returns bare `any`, so the declared Result is checked against nothing |
| `map-vs-struct` | 1 | `systemd.get_unit_info` — `map[string]any` against a declared struct |
| `progress-emitter` | 2 | `filebrowser.resource_patch`, `virt.create` — **legitimate and permanent** |

Verify the tally against the source of truth rather than trusting this table:
```bash
sed -n '/handleEventsInventory = map/,/^}/p' backend/bridge/handlers/handler_pattern_test.go \
  | grep -oE "duplicateModel|anyDomain|mapVsStruct|progressEmitter" | sort | uniq -c
```

Sampling showed the drifted routes are **wire-correct today** (`storage.mount_nfs`, `system.dismiss_*`, `wireguard.list_interfaces` all matched their declared struct exactly). This is a missing-enforcement problem, not a field of live bugs — so it does not block anything.

### Open decisions

**D1 — Duplicate models (18 routes): which side is authoritative?**
`accounts.UserDetails`/`UserLogin`/`Group`, `systemd.TimerStatus`/`SocketStatus`/`ServiceStatus`, `network.NetworkInterfaceInfo`, `storage.DriveInfo`, `packages.UpdateDetail`, `indexer.Status`, `appupdate.VersionInfo`, docker icon types, `system.get_host_info` (`*host.InfoStat`), `system.get_uptime` (`uint64` vs declared `float64`).
- (a) **apischema wins** — delete the local type, handlers build apischema types directly. *Recommended*: apischema is what the generated TypeScript derives from and what the frontend codes against, and it shrinks ToDo #8 rather than entrenching it. Cost: pairs that differ by pointer-ness (`FailedLoginAttemptsError string` vs `*string`) make construction sites noisier; no wire change, both are `omitempty`.
- (b) local wins — change the route declaration; generator picks it up.
- (c) alias where field-compatible, convert where not.

**D2 — `docker.list_volumes`: drop the extra Docker SDK fields?** Returns raw `[]volume.Volume`, carrying `Status` and `UsageData` beyond the declared `DockerVolume`. Generated TS never declared them; no frontend reference found. *Recommended: convert* — makes the wire match the contract instead of quietly exceeding it.

**D3 — `docker.list_networks`: same question.** Builds `[]map[string]any` against `[]apischema.DockerNetwork`. Key sets not yet compared; apply the D2 answer once they are.

**D4 — `systemd.get_unit_info`: convert the dynamic D-Bus map?** Every property the handler emits **is** declared in `UnitInfo` (verified), so no data loss. But values arrive as `any` from D-Bus, needing ~26 per-field type assertions, and D-Bus numerics don't always match (uint64 vs `*int64`). *Recommended: defer* — worst effort-to-value ratio in the set; the map is behaviourally correct.

**D5 — `filebrowser.subfolders`: drop `bytes`?** Local `subfoldersResponse` has `Bytes int64 \`json:"bytes,omitempty"\``; `apischema.SubfolderData` doesn't declare it. *Recommended: drop* — `omitempty`, absent from the TS type.

**D6 — `filebrowser.searchFiles`: which mod-time key?** `apischema.SearchResult` declares three variants (`mod_time`, `modTime`, `modified`), suggesting it was reverse-engineered from an inconsistent map. Need to know which the map actually sets and whether the other two should be deleted (a contract narrowing).

---

## Next: the actual goal

**The flip has not started.** Turning the ~107 progressless `Job` routes into Query/Action is the deliverable.

It also closes two findings that are still open:

- **Secrets in job snapshots.** No redaction exists anywhere in `ipc/bridge` or `apischema`. `Snapshot.Request` holds the decoded request verbatim for 30 minutes, served by `jobs.get` / `jobs.list` / `jobs.events`, owner-scoped by *username* (so any other session of that user). Four routes carry plaintext credentials — **three** (`accounts.change_password`, `accounts.create_user`, `storage.mount_cifs`) are `Job(...).Handle` routes the flip turns into Query, deleting the exposure rather than redacting it. Only `virt.create` needs real redaction, because it genuinely emits progress. (The result-side twin — the wireguard private key — is fixed.)
- **B7** (`CODE_REVIEW_FINDINGS.md`, the one confirmed correctness bug). `router.go` takes the goroutine path whenever `policy.Timeout > 0`, and `ActionDefault.Timeout` is 120 min — so on cancel the select returns, the job goes terminal, `finishJob` promotes the next queued job, and the handler goroutine is **still executing**. Real concurrency exceeds `MaxActivePerRoute` (empirically reproduced: 3 running at cap 2). Applies to every handler-form job route today; the flip removes it for the ~107 that stop being jobs.

### Suggested first batch: `accounts`

Nothing in it emits progress, nothing plausibly needs job semantics, and it contains two of the four credential routes.

Three questions per route — these are the actual work:

1. **Does it rely on `ActionDefault`'s accidental serialization** (`MaxActivePerOwnerRoute: 1`)? Decide explicitly rather than inherit it.
2. **Is it self-severing?** `control.reboot`/`power_off`/`logoff` and `network.set_ipv4*` destroy the connection by design and need the plan's `expected_loss` / `native_handoff` treatment before Query mode is safe. **Not in batch one.**
3. **Call-site change** — `useJobAction` → `useAction` for that handler's routes.

Two properties that make this safer than it looks:

- **It is self-policing.** `assertRouteMode` throws at render if a route's mode and hook disagree, so a missed call site is an immediate visible error, never silent breakage — provided backend, frontend and `make generate` land together per batch.
- **Query mode loses no detachment property.** The dispatch `ctx` is already bridge-scoped, not stream-scoped (`backend/bridge/cmd/yamux.go`), so a Query handler already survives browser stream reset exactly as a job does.

### Deliberately not doing

- Touching the transport. WebSocket + yamux is measured and fine.
- Starting the durable-jobs core (plan Stages 9–14). Durability beyond the bridge is *new* scope, not a restoration, and is not on the path to "React Query mutations stay clean."
- Adding `VoidQuery`/`VoidJob` constructors now. The design is right (compile-time void/value separation, no runtime panic) but it adds constructor vocabulary the plan already criticises, and would rewrite 62 declarations that the Action migration will rewrite anyway. Fold it into the `Action` contract at Stage 1.

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
cd backend && go build ./... && go test ./... -count=1 && cd .. && make golint-only
```

### Stale references to be aware of

`docs/jobs-architecture-migration-plan.md` lines 20 and 61 still cite `HandleWithPolicy` (deleted) and list the old `Handle/HandleWithPolicy/Run/Duplex` vocabulary. The plan's diagnosis and its `Query/Action/Duplex/FileTransfer/DurableJob` contract set remain sound; only those two lines describe a tree that no longer exists.
