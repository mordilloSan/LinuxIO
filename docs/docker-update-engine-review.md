# Docker Update Engine — Code Review

Point-in-time review of the Watchtower → native Docker update engine migration on `dev/v0.20.0`
(current source, tests, relevant history, and working tree; revalidated 2026-08-13).
Method: independent review angles reconciled against the current source and tests.
Delete this file once the findings below are resolved.

**Totals:** 23 tracked findings — 15 active (1 high, 8 medium, 6 low; 13 confirmed / 2
plausible), 6 resolved, 1 deferred Engine-API limitation, and 1 confirmed behavior with no
current product defect.

## Summary

The migration is structurally sound — the oneshot systemd-timer model, shared cross-process lock,
stop → rename → create → verify → remove rollback choreography, and packaging wiring all hold
up. F01, F02, F04, F06, F10, and F16 are now resolved. The most important remaining risks are
running manual mutations inside the bridge process (F18) and several Compose/standalone ownership
gaps (F19–F22).

F03 remains a real fidelity limitation, but its original fix was unsafe: Docker Inspect exposes
an endpoint MAC that may be generated operational state, not configured intent. Blindly copying
it can pin generated MACs and create duplicate-address failures. Safe preservation requires an
explicit configured-intent source or separately stored recreation metadata.

## Decision record — scheduled lifecycle

An earlier design required an hourly timer coupled to `linuxio.target`. That requirement was
intentionally superseded. The supported design is a user-configurable daily maintenance time, a
timer enabled directly under `timers.target`, and a dedicated short-lived
`linuxio-docker-update` worker that requires Docker but does not depend on the bridge process.
This keeps scheduled host mutation independent of the ephemeral application process and avoids a
resident updater daemon. The older hourly/`linuxio.target` requirement is obsolete and is not an
open finding.

## Findings index

Tracked in stable ID order; severity and disposition are explicit in each row.

| ID  | Sev    | Verdict   | Finding                                                        | Location                            |
| --- | ------ | --------- | -------------------------------------------------------------- | ----------------------------------- |
| F01 | High   | Resolved  | `--tmpfs` containers can never be updated                       | `native_update_apply.go:483`        |
| F02 | High   | Resolved  | Backup-cleanup failure clobbers a successful update's status    | `scheduled_update_apply.go:68`      |
| F03 | High   | Deferred  | Pinned MAC intent is unavailable from Docker Inspect            | `native_update_apply.go:425`        |
| F04 | High   | Resolved  | Local images fail every scheduled update-mode run               | `native_update_check.go:157`        |
| F05 | Medium | Confirmed | Orphaned backup containers are never swept                      | `native_update_apply.go:327`        |
| F06 | Medium | Resolved  | Replacement inherits the old container's short-ID hostname     | `native_update_apply.go:417`        |
| F07 | Medium | Plausible | Compose hard-errors the benign same-image outcome               | `native_update_apply.go:252`        |
| F08 | Medium | Confirmed | Worker logs unstructured; component tag breaks convention       | `cmd/linuxio-docker-update/main.go:20` |
| F09 | Medium | Confirmed | Scheduled orchestration and lock remain largely untested        | `scheduled_update_runner.go:24`     |
| F10 | Medium | Resolved  | `.gitignore` missed the new binary at the repo root             | `.gitignore:13`                     |
| F11 | Low    | Confirmed | Manual and scheduled paths duplicate decision + verify logic    | `scheduled_update_apply.go:99`      |
| F12 | Low    | Confirmed | Duplicated helper, lock constants, and JSON-document pattern    | `native_update_apply.go:516`        |
| F13 | Low    | Confirmed | Sequential registry round-trips on the UI refresh path          | `native_update_check.go:162`        |
| F14 | Low    | Confirmed | Per-update full-host dependent inspection                       | `native_update_apply.go:364`        |
| F15 | Low    | No defect | `docker_updates` capability alone ignores Docker availability   | `system/capabilities.go:71`         |
| F16 | Low    | Resolved  | Plan doc was a stale status record                              | `docs/docker-update-engine-plan.md` |
| F17 | Low    | Plausible | First not-running poll aborts with no retry                     | `native_update_apply.go:532`        |
| F18 | High   | Confirmed | Manual update mutation still runs inside the bridge process     | `updates.go:92`                     |
| F19 | Medium | Confirmed | Compose invocation environment cannot be reconstructed          | `native_update_apply.go:130`        |
| F20 | Medium | Confirmed | Replica selection silently broadens to the whole Compose service | `scheduled_update_apply.go:157`    |
| F21 | Medium | Confirmed | Standalone dependency checks miss IPC/PID/cgroup namespaces     | `native_update_apply.go:364`        |
| F22 | Medium | Confirmed | Stopped standalone and Compose targets have conflicting behavior | `scheduled_update_runner.go:52`    |
| F23 | Low    | Confirmed | Successful check toasts hide partial scan errors                | `useDockerUpdateCheck.tsx:14`       |

Backend paths are relative to `backend/bridge/handlers/docker/` unless noted.

## Findings

### F01 — `--tmpfs` containers can never be updated

**High · Resolved · `native_update_apply.go:483` · correctness**

The original implementation checked `HostConfig.Mounts` and `HostConfig.Binds` but not
`HostConfig.Tmpfs`, so a tmpfs entry surfacing in `inspect.Mounts` fell into
`preserveInspectedMounts`' refusal path even though the cloned `HostConfig.Tmpfs` already recreated
the mount correctly.

**Resolution:** commit `d74c472b` added the `HostConfig.Tmpfs` lookup and
`TestStandaloneCreateOptionsPreservesTmpfs`. The current recreation path neither rejects nor
duplicates a configured tmpfs mount.

### F02 — Backup-cleanup failure clobbers a successful update's status

**High · Resolved · `scheduled_update_apply.go:68` · correctness**

The original scheduled path handled the cleanup error before the `Updated: true` result. A
transient removal failure after a successful replacement therefore persisted an error against the
pre-update inspect and overwrote the correct current status. The manual RPC still reports this
cleanup-only error to its caller, which is asserted by its existing test.

**Resolution:** commit `384fac6b` routes the outcome through `recordStandaloneUpdateOutcome`, which
retains the replacement's current status, tracks the old image, and aggregates/logs the cleanup
failure without overwriting the status with the old container. The regression test is
`scheduled_update_apply_test.go`.

### F03 — Pinned MAC intent is unavailable from Docker Inspect

**High · Deferred Engine-API limitation · `native_update_apply.go:425` · correctness**

The rebuilt per-network `EndpointSettings` does not copy `MacAddress`, so a standalone container
created with an explicitly pinned per-network MAC may receive a different address after
recreation. However, the inspected endpoint value is operational state and may be Docker-generated.
API v1.55 does not expose reliable container-wide configured-MAC intent. Copying every non-empty
inspected address would pin generated values and can create duplicate-MAC/network failures after
recreation or daemon restart.

**Required before implementation:** obtain explicit configured intent — for example, LinuxIO-owned
recreation metadata captured when the user opts into standalone updates — and copy only that
configured value. Do not copy `NetworkSettings.Networks[*].MacAddress` blindly. Until a reliable
source exists, this remains a documented standalone-update limitation rather than a safe contained
fix.

### F04 — Local images fail every scheduled update-mode run

**High · Resolved · `native_update_check.go:157` · correctness**

Previously, an image with no `RepoDigests` (anything from `docker build` that was never pushed) was
recorded as a hard error on every check. In the default scheduled `update` mode that error
propagated through `errors.Join` to the process exit code, so `linuxio-docker-update.service` was
marked failed on every firing. In `check_only` mode the worker returned success but left the same
permanent error badge.

**Resolution:** local-only images now produce an explicit persisted/API `uncheckable` state and
informational reason. They increment the result's `uncheckable` count instead of `errors`, do not
enter the scheduled update run's joined error, and leave `updateAvailable` absent in container and
image API responses. Container and image views render "Cannot check", and interactive checks give
warning feedback instead of claiming the image is current. Legacy status records infer
`current`/`available`/`error` without a status-file version break. Backend coverage exercises the
scan result, API projection, image projection, and scheduled update-mode path; frontend coverage
pins the container-row label.

### F05 — Orphaned backup containers are never swept

**Medium · Confirmed · `native_update_apply.go:327` · correctness**

The renamed `linuxio-update-backup-*` container is removed at exactly one call site. If that
removal fails, nothing ever retries or sweeps it: later updates of the same container use a new
backup name (derived from the new ID), so stopped backup containers accumulate in `docker ps -a`.
Each one also keeps its old image in use, which permanently blocks `cleanupUnusedUpdateImages` from
pruning that image.

**Fix:** persist enough transaction/recovery identity to distinguish a verified leftover from the
only recoverable original container, then retry safe cleanup at the start of later runs. Do not
blindly delete every stopped container with the prefix: after process death mid-transaction, that
container may be the rollback source.

### F06 — Replacement inherits the old container's short-ID hostname

**Medium · Resolved · `native_update_apply.go:417` · correctness**

`cloneJSON(inspect.Config)` copies `Hostname` verbatim. A container that never set `--hostname` has
Docker's default — its own 12-character ID — so the replacement is created with the *old*
container's ID pinned as an explicit hostname, naming a container that no longer exists.
Inconsistent with the deliberate endpoint-settings handling a few lines below.

**Resolution:** `standaloneCreateOptions` now clears the cloned hostname when it equals the old
container ID's 12-character prefix and the container is not using host networking. Docker then
assigns the replacement's own short ID. Explicit hostnames and host-network hostnames remain
unchanged, with table-driven regression coverage.

### F07 — Compose hard-errors the benign same-image outcome

**Medium · Plausible · `native_update_apply.go:252` · consistency**

Both paths run only after the digest check reports an update. When the subsequent pull resolves to
the image already running (a digest-check false positive — e.g. a registry or mirror manifest
quirk), the standalone path marks the container current and succeeds, while the Compose path
returns "completed without activating the pulled image" as a hard failure and never calls
`markContainerCurrent` — so the container re-fails on every scheduled run until the registry state
changes.

**Fix:** mirror the standalone behavior: when `after.Image == before.Image` post-pull, mark current
and return success instead of erroring.

### F08 — Worker logs unstructured; component tag breaks convention

**Medium · Confirmed · `backend/cmd/linuxio-docker-update/main.go:20` · consistency**

The new binary never calls `logging.Configure`, so its `slog` output uses the default text handler
on stderr — no journald handler, no `LINUXIO_` structured fields, no `AddSource`, no debug level —
unlike the bridge and webserver. Separately, its five log sites tag `"component","docker-update"`
while normal `handlers/docker` logs use `"component","docker"` plus a `subsystem` tag, so
any journal query filtering `component=docker` silently misses every scheduled-update line.

**Fix:** call `logging.Configure("linuxio-docker-update", …)` in `main()`, and retag the five sites
as `component=docker, subsystem=update` via a package constant.

### F09 — Scheduled orchestration and lock remain largely untested

**Medium · Confirmed · `scheduled_update_runner.go:24` · test-coverage**

The migration deleted Watchtower's runner and lock tests without equivalent replacements.
`scheduled_update_apply_test.go` now covers F02's standalone cleanup-status outcome, so the original
"zero scheduled tests" claim is stale. However, missing-container detection, Compose-group
batching, per-container error isolation, image cleanup, check-only behavior, and lock contention
still lack unit coverage. `acquireDockerUpdateLock` also hardcodes
`/run/linuxio-docker-update.lock` (Watchtower's equivalent took the path as a parameter), so lock
contention cannot be exercised hermetically.

**Fix:** re-parameterize the lock path (or a package variable overridable in tests) and port the
deleted Watchtower test patterns: second-holder exclusion, error aggregation, and the F02
interaction.

### F10 — `.gitignore` missed the new binary at the repo root

**Medium · Resolved · `.gitignore:13` · hygiene**

The original migration built `linuxio-docker-update` at the repository root without adding the
corresponding ignore entry, making the generated executable easy to stage accidentally.

**Resolution:** commit `d6e0a800` added `/linuxio-docker-update` and removed the tracked build
artifact. A locally built worker is ignored in the current tree.

### F11 — Manual and scheduled paths duplicate decision + verify logic

**Low · Confirmed · `scheduled_update_apply.go:99` · simplification**

Two parallel re-implementations: `inspectScheduledUpdateCandidate` duplicates
`updateInspectedContainer`'s normalize → immutable-check → digest-check → mark-current decision
(~25 lines), and `verifyScheduledComposeContainer` duplicates `updateComposeContainer`'s inspect /
wait-ready / image-compare tail (~15 lines). Any change to "does this container need an update" or
post-update verification must now be made twice or the on-demand and scheduled behaviors silently
diverge — F07 is already a divergence of this kind.

**Fix:** extract a shared update-decision helper both entry points call, and have
`updateComposeContainer` reuse the verify function.

### F12 — Duplicated helper, lock constants, and JSON-document pattern

**Low · Confirmed · `native_update_apply.go:516` · reuse**

Three small copies in one package: `cloneEndpointStringMap` reimplements the existing
`cloneStringMap` (`volumes.go:74`) with a subtly different nil guard; the 10s/250ms lock wait/poll
constants duplicate `updateStatusLockWait/Poll`; and the versioned-JSON document read/write shape
is hand-copied between `auto_update_native.go` and `update_status_store.go` — a third status/config
file will most likely copy-paste a third.

**Fix:** delete `cloneEndpointStringMap` in favor of `cloneStringMap`; share the lock tuning
constants; consider a small versioned-document helper when the next JSON file appears.

### F13 — Sequential registry round-trips on the UI refresh path

**Low · Confirmed · `native_update_check.go:162` · efficiency**

`checkContainerImageUpdates` performs one `DistributionInspect` registry round-trip per unique
`(image ID, normalized reference)` cache key, strictly sequentially — on the interactive
`RefreshDockerImageUpdates` request path as well as scheduled runs. Thirty distinct keys at a few
hundred milliseconds each can put roughly ten seconds on the UI's update check.

**Fix:** bounded-concurrency fan-out (`errgroup.SetLimit(4–8)`) over the unique image cache keys;
observations are independent.

### F14 — Per-update full-host dependent inspection

**Low · Confirmed · `native_update_apply.go:364` · efficiency**

`validateStandaloneDependents` lists all containers and then `ContainerInspect`s every other
container sequentially, before *each* standalone update — a scheduled pass updating M standalone
containers on an N-container host issues roughly M×N inspect RPCs to re-derive the same
network/volumes-from facts.

**Fix:** build the dependency map once per scheduled run (or per update, concurrently) and share it
across candidates.

### F15 — `docker_updates` capability alone ignores Docker availability

**Low · Confirmed behavior, no current defect · `backend/bridge/handlers/system/capabilities.go:71`**

Availability is solely "the `linuxio-docker-update` binary exists and is executable" — and the
binary normally ships with the base install. On a host with no reachable Docker daemon this flag
can still be true. Current UI consumers also require the Docker route/capability before presenting
the scheduling controls, and the configuration call itself reports Docker list failures, so no
current misleading product path was demonstrated.

**Revisit if reused independently:** make the capability depend on both worker installation and
Docker availability, or rename it to communicate that it reports worker installation only.

### F16 — Plan doc is a status record, not a contributor guide

**Low · Resolved in working tree · `docs/docker-update-engine-plan.md` · convention**

The removed document opened with an implementation-complete status and encoded delivery as checked
batches and completion criteria. That point-in-time status had already gone stale as the engine
changed.

**Resolution:** the stale implementation-status document has been removed in the current working
tree. Durable decisions needed for follow-up work are recorded in this review instead.

### F17 — First not-running poll aborts with no retry

**Low · Plausible · `native_update_apply.go:532` · correctness**

`waitForContainerReady` treats the very first observation of `!State.Running` as terminal, while a
health check in "starting" state is patiently retried. If an inspect ever races a just-started
container, the update rolls back unnecessarily. Confidence is low — `ContainerStart` is
synchronous — but while touching this: the inline terminal condition duplicates `containerReady`'s
logic, and its `Health == nil` arm is unreachable dead code.

**Fix:** have `containerReady` return a (ready, terminal) pair both call sites switch on, keeping
the two conditions permanently in sync.

### F18 — Manual update mutation still runs inside the bridge process

**High · Confirmed · `updates.go:92` · reliability / ownership**

`docker.update_container` is registered as a synchronous bridge `Call`. Its handler acquires the
global update lock and performs pull, stop, rename, create, start, readiness verification, and
cleanup using the request path. A registry pull plus readiness wait can outlive the browser request,
and a bridge restart or process death can interrupt the host mutation. Rollback correctly uses a
short `context.WithoutCancel` context, which mitigates request cancellation after an error, but it
cannot run if the bridge process itself dies. Scheduled mutation already uses the dedicated
systemd-owned worker, so the two entry points have different lifecycle guarantees.

**Fix:** route manual mutation through a durable, system-owned worker/job outside the bridge while
retaining the same flock and status store. Return a typed operation identity/state to the frontend.
Detaching only the request context is not sufficient because it still cannot survive process death.

### F19 — Compose invocation environment cannot be reconstructed

**Medium · Confirmed · `native_update_apply.go:130`, `compose_sdk.go:77` · correctness**

Compose discovery preserves project name, config-file paths, and working directory from labels.
The later `docker compose` call reconstructs only `--project-name`, repeated `--file` arguments,
and `cmd.Dir`. It cannot recover the original custom `--env-file`, exported shell variables, or
relevant `COMPOSE_*` environment. A project using `${IMAGE_TAG}` or other interpolation can
therefore resolve to a different model under the scheduled worker, updating the wrong image or
changing ports/volumes, or fail to render at all. A default `.env` in the preserved working
directory may work, but custom invocation context is not encoded in standard container labels.

**Fix:** capture the required environment/env-file provenance when a Compose service is enrolled
for updates and pass it explicitly on every reconstructed invocation. If provenance is unavailable,
refuse scheduled mutation with a clear actionable status rather than guessing. Cover a custom
env-file and interpolated image tag in the Compose integration suite.

### F20 — Replica selection silently broadens to the whole Compose service

**Medium · Confirmed · `scheduled_update_apply.go:157`, `compose_sdk.go:172` · correctness / UX**

Auto-update configuration selects container names, including individual Compose replicas. The
scheduled preparation path converts selected replicas to service names, deduplicates them, then
runs `docker compose pull` and `up -d --no-deps SERVICE`. Compose reconciles the service as a whole,
so selecting one replica can recreate every replica. Verification and persisted status updates
iterate only the originally selected containers, leaving unselected replicas with stale update
status even though they may have changed.

**Fix:** model Compose enrollment explicitly at project/service scope and make the UI show that
scope. After apply, re-list and verify every replica in the affected service and refresh all of
their statuses. Add a scaled-service integration test in which only one replica was initially
selected.

### F21 — Standalone dependency checks miss IPC/PID/cgroup namespaces

**Medium · Confirmed · `native_update_apply.go:364` · correctness**

Before recreating a standalone provider, `validateStandaloneDependents` rejects dependents using
`NetworkMode=container:<ref>` or `VolumesFrom`. Docker also supports container references in
`IpcMode`, `PidMode`, and `Cgroup`, each with API helpers for resolving the target. Those references
are not checked. Recreating the provider changes its ID and namespace ownership while dependent
containers still refer to the old instance, defeating the validator's stated safety boundary.

**Fix:** centralize container-reference matching and apply it to network, IPC, PID, and cgroup
namespace modes plus `VolumesFrom`. Add one regression case per supported reference form, including
name, full ID, and 12-character ID matching where applicable.

### F22 — Stopped standalone and Compose targets have conflicting behavior

**Medium · Confirmed · `scheduled_update_runner.go:52` · correctness / semantics**

Target discovery uses `ContainerList(All: true)`, and the UI/backend allow stopped containers to be
enrolled. In update mode, standalone validation rejects a stopped container and records a recurring
scheduled error. The Compose path has no equivalent guard and invokes `docker compose up -d`, which
can start a deliberately stopped service. Thus the same setting either fails forever or changes
runtime state depending only on ownership type. Check-only mode still scans both.

**Fix:** choose and expose one policy. The conservative policy is to allow checks but skip mutation
for stopped targets, preserve a non-error informational state, and disable/annotate enrollment in
the UI. If stopped Compose services are intentionally startable, make that explicit and apply the
same documented semantics to standalone targets. Test both ownership paths.

### F23 — Successful check toasts hide partial scan errors

**Low · Confirmed · `useDockerUpdateCheck.tsx:14`, `ContainerTable.tsx:349` · observability**

The backend result includes `checked`, `updates`, and `errors`, and increments `errors` for each
target-level scan failure. Both the global check hook and per-container check callback ignore that
field and always show a success/up-to-date message. Registry failures and malformed/local-only
image failures can therefore produce a green success toast while row status records an error.

**Fix:** show warning/error feedback whenever `result.errors > 0`, include the count in the message,
and never say a single container is up to date when its check failed. Add component/hook tests for
full success, partial failure, and total per-container failure. Preserve F04's non-error
`uncheckable` wording.

## Claims investigated and rejected

Raised during review, then refuted against source or documented semantics — recorded so they aren't
re-litigated later.

- **"Unhealthy containers should be retried, not failed immediately."** Docker only reports
  `unhealthy` after the container's own configured retries and start period are exhausted —
  `waitForContainerReady` failing fast on it is correct Docker semantics, not an oversight.
- **"The second `ContainerList` in `cleanupUnusedUpdateImages` is redundant."** The re-list is
  required for correctness: the in-use image set must reflect *post-update* state. Reusing the
  pre-update listing would see old images as still in use and never prune them.
- **"Missing version ldflags break the new binary's version reporting."** `version.BinDir`/`DataDir`
  are compile-time constants, not injected, and the binary has no version output.
  `build-docker-update` using bare `-s -w` is a consistency nit that matters only if a `version`
  subcommand is ever added.

## What holds up

- Packaging is complete and consistent: the binary is in the release workflow,
  download/install/verify lists, localinstall, and uninstall's `linuxio*` glob already covers it.
- Removing the legacy Watchtower migration (code, CLI subcommands, install-script invocations)
  matches the documented decision and left no dangling references.
- Manual and scheduled updates share one `flock`-backed lock at `/run/linuxio-docker-update.lock` —
  no dual-scheduler or ad-hoc locking problem.
- The standalone rollback choreography (stop → rename → create → start → verify → remove, rollback
  at each step with `context.WithoutCancel`) is sound.
- The oneshot systemd-timer model is preserved — no resident daemon; the worker exits after each
  pass.
- Compose SDK changes (multi-file config support, generalized `runComposeProject`) are genuine
  generalizations, not special cases.

## Suggested order of work

Work one independently reviewable finding at a time:

1. **F06 — completed.** Regenerate only default short-ID hostnames and preserve
   explicit/host-network values.
2. **F04 — completed.** Local images now carry an honest non-error `uncheckable`
   state across persisted status, API results, scheduled execution, and the frontend.
3. **F18.** Move manual mutation to a durable system-owned execution path before expanding update
   behavior further.
4. **F21**, then **F19**, **F20**, and **F22** individually. These close the remaining namespace,
   Compose-provenance, service-scope, and stopped-state correctness gaps.
5. **F05.** Design recovery metadata before adding any backup sweep.
6. **F08**, **F09**, **F07**, then **F23** as separate observability, coverage, parity, and feedback
   changes.
7. Measure **F13/F14** before optimizing. Take **F11/F12** only when they simplify an active fix,
   and reproduce **F17** before changing readiness semantics.

F03 is intentionally deferred until configured MAC intent is available. Backend-only fixes require
`make check-backend`; changes spanning the API/frontend require `make generate` followed by
`make test`; Compose apply behavior additionally requires the opt-in
`make test-docker-update-integration` gate when a Docker daemon and Compose plugin are available.
