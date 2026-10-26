# Jobs: mutations that outlive the login

Date: 2026-10-26. Status: draft for review. Companion: [the mutation map](2026-10-25-mutation-map.md).

## 1. Goal

Work a user starts from the UI must not depend on that user staying logged in,
on the browser tab staying open, on the per-login bridge staying alive, or on
LinuxIO not updating itself. A scrub, a copy, a stack coming up or a package
update runs to the end, and the user finds it again at the next login, with its
progress, its outcome and its log.

At the same time, a programmer writing a mutation must not have to decide
whether it is "fast" or "slow". There is one way to write a mutation and one
way to call it.

Non-goals: resuming non-storage work after a reboot or power loss (it is
reported as interrupted); moving uploads and downloads out of the browser
connection; changing how reads work; changing authentication.

## 2. Decisions in one page

| Decision | Choice |
|---|---|
| API shape | `Call` means read. Every write is a `Mutation`. One stream carries request, progress and result. One round trip. |
| Client timeout | None for mutations. Connection loss never cancels a mutation. Reads keep the 30 s timeout and the retry policy. |
| Where mutations run | In a **job host**: one long-lived process per uid, as that uid (root for admins), started at login, exiting when no bridge is connected and no job runs. |
| Who starts hosts | A **launcher**: root, socket-activated, one instance per request, no handler code. It can only start `linuxio-jobs@<peer uid>`. |
| Identity | The kernel's `SO_PEERCRED` on every socket. A claim from a bridge is metadata, never authorization. |
| Same target twice | Refused with 409 while the first runs. No queue, no per-user or per-route cap, no runtime cap. |
| Reboot and power loss | Running jobs become `interrupted`. Resumable ones (btrfs scrub, balance, replace, remove) are restarted by the boot service; the rest alert. |
| Orderly shutdown | Resumable jobs pause and checkpoint on SIGTERM so the restart continues where they were. |
| Updates | Hosts are not part of `linuxio.target` and are never restarted by the installer. An old host finishes its jobs and exits when idle. |
| History | On disk, 30 days or 200 jobs per user. Jobs that succeed in under a second are not kept. |
| Alerts | A failed or interrupted job raises an alert (admin hosts). Success goes to history only. |
| Scheduled scripts | Stay systemd timers and services. They appear in the same list and history and use the same alert hook. |
| Jobs with no user | Timers and the boot service run a job as its own unit through the same handler code (`jobs run`). Same record, same list. |
| Logs | journald. Host jobs carry a `JOB_ID` field; unit jobs are their own unit. |
| Rejected | A system-wide root daemon running jobs (TrueNAS shape): it would run non-admins' work as root. A process per job started from the bridge: 15 ms each, which forces a fast/slow decision per route. ZFS: out-of-tree module on the user's distro. |

## 3. Terms

- **Bridge**: today's per-login process, as the user. Keeps reads, terminals, uploads and downloads. Relays mutations to the host.
- **Host**: `linuxio-jobs@<uid>.service`. Runs every mutation for that uid as a goroutine. Owns the in-memory job registry for that uid and writes the store.
- **Launcher**: `linuxio-jobs-launcher.socket` plus `linuxio-jobs-launcher@.service`. Starts a host for the caller's uid.
- **Worker**: `linuxio-bridge jobs run --id <id>`, the same handler code run as its own unit `linuxio-job@<id>.service` when there is no user: timers, the boot service.
- **Store**: `/var/lib/linuxio/jobs/<uid>/`, one JSON record per job, owned by the uid.
- **Job**: a record plus a handler run. Started by a user through the host, or by the system through a worker.
- **Target**: what a job works on (pool UUID, disk, container, compose project, set of paths), declared by the route.

## 4. Architecture

```
browser ──WebSocket── linuxio-webserver (unprivileged) ──yamux── bridge (per login, as the user)
                                                                     │  reads, terminals,
                                                                     │  upload/download streams
                                                                     │
                                            every mutation stream, relayed unchanged
                                                                     ▼
   launcher (root, socket-activated) ──StartUnit──▶ systemd ──▶ linuxio-jobs@<uid>.service
        ▲  "start my host" at login                               host, as <uid>
        │                                                         ├─ job registry (memory)
   bridge, once per login                                         ├─ handlers (shared code)
                                                                  └─ store /var/lib/linuxio/jobs/<uid>/
                                                                       live progress /run/linuxio/jobs/<uid>/

   systemd timers, boot service ──▶ linuxio-job@<id>.service  (worker: jobs run --id)
                                       same handlers, same store, root
```

The webserver, `linuxio-auth` and the bridge keep their roles and boundaries.
Nothing new listens on the network. The two new root pieces are the launcher
(starts one unit) and the root host, which runs admin mutations as root exactly
as the root bridge does today.

## 5. Programming model

### Backend

```go
// A read: retry-safe, 30 s client timeout, cached by TanStack Query.
apischema.Call[Req, Res]("docker.list_containers", apischema.RetrySafe()).Handle(list)

// A mutation. No fast/slow decision.
apischema.Mutation[Req, Res]("docker.start_container").Handle(start)

// A mutation with progress, a target, and resume after reboot.
apischema.Mutation[Req, Res]("storage.btrfs_scrub_start",
    apischema.Privileged(), apischema.Resumable(scrubResume),
    apischema.Target(func(r Req) []string { return []string{r.UUID} }),
).Run(scrub)
```

- `Handle` binds `func(ctx, req) (res, error)`; `HandleVoid` binds `func(ctx, req) error` for `NoResponse`; `Run` binds `func(ctx, req, report func(TaskProgress)) (res, error)`. Existing handlers keep their signatures.
- `Target` replaces today's `WithTaskMetadata`: the identity used for the same-target refusal and shown in the job list.
- `Resumable(hooks)` gives the route a `Suspend(ctx, record) (checkpoint, error)` and a `Resume(ctx, record, report) (res, error)` (section 11). It is valid only on `Privileged` routes.
- `TaskRunner`, `SessionTask`, `DurableTask`, `WithTaskLifetime`, `WithTaskIdentity` and the `TaskPolicy` admission are removed.
- Three routes keep the bridge: `filebrowser.upload`, `filebrowser.upload_batch`, `filebrowser.archive`. They are declared `Mutation` with `apischema.BrowserBound()`, which the generator and the relay use to keep them local. This is the only exception and it is mechanical: the bytes come from or go to the browser.

### Guard tests

- `Call` routes must be `RetrySafe`. The eight reads that are not marked today (`docker.get_icon`, `docker.get_icon_uri`, `docker.check_updates`, `docker.check_container_update`, `monitoring.check_database`, `system.get_health_summary`, `network.get_network_info`, `terminal.list_shells`) are marked, after confirming each has no side effect worth refusing a retry for.
- Every `Mutation` appears in a checked inventory table with its `Resumable` and `BrowserBound` flags, the way `handleEventsInventory` worked. Adding a mutation without a row fails the test.
- A handler package may not import the launcher package and the launcher package may not import handlers (`import_boundary_test.go`).

### Frontend

One generated hook per mutation, `linuxio.<domain>.<name>.useMutation(config)`, with the `ActionConfig` of today's `useCallMutation` plus optional `onProgress(progress, snapshot)` and `onStart(snapshot)`. A promise form `linuxio.<domain>.<name>(request, { onProgress, signal })` exists for non-component code. `useCallMutation`, `useTaskAction` and `useTaskStreamAction` are removed. Query invalidation on success keeps using `operation-query-invalidations.ts`.

## 6. Wire protocol

One stream per mutation. The envelope gains an `id`:

```json
{"route":"docker.start_container","request":{"containerId":"abc"},"id":"5d2a…-uuid"}
```

1. The client generates `id` (UUID v4) per mutation call and sends it with the request.
2. The server answers at once with a `TaskSnapshot` frame (the job exists, the ID is claimed), then zero or more `TaskProgress` frames, then exactly one result or error frame, then closes. These are the `tasks.watch` frames of today; the start stream *is* the watch.
3. Closing the stream detaches; the job continues. An explicit abort (`OpStreamAbort`) cancels. Connection loss detaches.
4. Resend after `outcome_unknown`: the client sends the same envelope. The host finds the `id`, attaches to the running job or returns its terminal snapshot. Terminal snapshots stay in host memory for 30 minutes and in the store for the retention period. A resend with the same `id` but a different route or request is a 409.
5. Reattach later: `tasks.watch {taskId}`, from any tab or login of the same uid. `tasks.events` for the owner-wide list, `tasks.list`, `tasks.get`, `tasks.cancel` keep their contracts and are served by the host through the relay. `tasks.data` stays in the bridge for the browser-bound routes.

No client-side timeout for mutations. The 25 ms settle wait and `InitialTaskSettleTimeout` are removed.

## 7. Bridge to host relay

- At startup the bridge asks the launcher to start its host (section 9), then connects to `/run/linuxio/jobs/<uid>/host.sock` and keeps one yamux session for its lifetime. If the connect fails it retries with backoff and reports mutations as `connection_unavailable` meanwhile. Measured cost per mutation on the development machine: 27 µs median, 55 µs p99.
- The first stream is a hello: `{protocol: N, session: {username, uid, privileged, session_ref}}`. The host checks the peer uid with `SO_PEERCRED`: a user host accepts only its own uid; the root host accepts only uid 0. The session facts are recorded as the job's owner for listing, history and alerts; they are not authorization.
- Each mutation opens one stream; the bridge forwards frames in both directions unchanged and closes when either side closes. Abort forwards as abort.
- The bridge serves itself: all `Call` routes, duplex channels (terminals, log follows), the three browser-bound routes and their `tasks.data`.
- Session end does not cancel anything in the host. The bridge closes its yamux session; the host counts one connected bridge fewer.

## 8. Host lifecycle

Unit template `linuxio-jobs@.service`, instance = uid:

```ini
[Unit]
Description=LinuxIO jobs for uid %i
After=local-fs.target
# Deliberately not PartOf=linuxio.target: updates must not stop running jobs.

[Service]
Type=notify
User=%i
ExecStart=/usr/local/bin/linuxio-bridge jobs host
StateDirectory=linuxio/jobs/%i
RuntimeDirectory=linuxio/jobs/%i
RuntimeDirectoryPreserve=yes
TimeoutStopSec=2min
KillMode=mixed
NoNewPrivileges=yes
```

- **Start**: by the launcher when a bridge logs in; nothing else starts a host. `StateDirectory` and `RuntimeDirectory` create the store and socket directories owned by the uid; no custom code creates them.
- **Stay up while** connected bridges > 0 or running jobs > 0.
- **Exit** when both are zero for one minute (so a logout and login within seconds does not restart it). Exit is `sd_notify(STOPPING=1)` then process exit; systemd collects the unit.
- **Stop by systemd** (shutdown, or `systemctl stop`): SIGTERM. The host marks every running job: resumable ones get up to `TimeoutStopSec` to suspend (section 11), the rest are marked `interrupted` with reason `shutdown`. Then it exits.
- **Crash**: the next host for that uid, or the boot service, finds records in `running` whose executor pid and start time are not alive and marks them `interrupted` with reason `crash`.
- **Limits**: a host refuses a new job above 256 running jobs for its uid with 409. This is a safety valve against a runaway client, not a UX limit.

## 9. Launcher

`linuxio-jobs-launcher.socket` listens on `/run/linuxio/jobs-launcher.sock`, mode `0666`, `Accept=yes`; `linuxio-jobs-launcher@.service` runs as root, `ExecStart=/usr/local/bin/linuxio-bridge jobs launch`, one process per connection, exits after answering.

Protocol: the client sends nothing. The launcher:
1. reads the peer uid, gid and pid with `SO_PEERCRED`;
2. resolves the uid with `getpwuid`; refuses an unknown uid and any user in `/etc/linuxio/disallowed-users`;
3. calls `StartUnit("linuxio-jobs@<uid>.service", "replace")` over the system bus; starting a running unit is a no-op;
4. waits up to 5 s for `/run/linuxio/jobs/<uid>/host.sock` to appear;
5. replies one line, `ok` or `error: <reason>`, and exits.

It never reads a unit name, a uid or any property from the client. The only thing a caller can make it do is start the caller's own host. The launcher package imports `peercred`, `systemd` and `logging`, nothing from `handlers`.

## 10. Store and records

Layout per uid, created by systemd:

```
/var/lib/linuxio/jobs/<uid>/<id>.json         record, 0600
/var/lib/linuxio/jobs/<uid>/<id>.result       bounded result artifact, 0600, when the result exceeds 64 KB
/run/linuxio/jobs/<uid>/<id>.progress         live progress, memory-backed, rewritten at most twice a second
/run/linuxio/jobs/<uid>/host.sock
```

Record fields: `id`, `route`, `owner {uid, username}`, `target []string`, `state`, `reason` (for `interrupted`: `shutdown`, `crash`, `reboot`), `created_at`, `started_at`, `finished_at`, `request` (the full decoded request, needed to run or resume; never returned by any API), `progress` (last envelope), `result` or `error {code, message}`, `executor {kind: host|unit, pid, pid_start, unit, invocation_id}`, `resumable`, `checkpoint` (opaque per route, section 11), `protocol`.

States: `running`, `completed`, `failed`, `canceled`, `interrupted`. There is no `queued`: admission either starts the job or refuses it.

Write policy, from the spike measurements (1.4 ms fsync, 35 µs without, on NVMe):
- a record is written when the job starts and on every state change;
- fsync on state changes of jobs that have run for more than one second, and on every checkpoint; no fsync otherwise;
- progress goes to `/run`, never to `/var/lib`;
- a job that completes in under one second is deleted from the store when it completes, unless it failed. Such jobs never appear in the list or history. This is TrueNAS's `transient` without a flag.

Retention: terminal records are kept 30 days and at most 200 per uid; pruning runs at host start and daily, removing artifacts with records. This is today's `durabletask` policy; the package is generalised rather than replaced.

## 11. Job lifecycle

**Claim.** The host validates the request with the route decoder, builds the target, and checks the `id`: unknown → new job; known with the same route and request → attach; known with a different route or request → 409. Then admission: a running job of the same route with the same target → 409 `"<route> is already running on this target"`. Admission is per host, which is per uid; for admins the root host is shared, so privileged targets (pools, disks, containers, Docker, packages) are checked system-wide.

**Run.** The handler runs in a goroutine with a context cancelled only by `tasks.cancel`, stream abort, or host stop. `report` writes a progress frame to every attached stream and to the `/run` file (coalesced). The handler's log lines carry `JOB_ID=<id>`, `JOB_ROUTE=<route>` as journald fields through the existing slog handler.

**Finish.** The result frame goes to attached streams; the record is written `completed` or `failed`; a failed job raises an alert (section 13). A `completed` job younger than one second is dropped from the store.

**Cancel.** `tasks.cancel` or an abort sets `cancel_requested` in the record *first*, then cancels the context. The handler's own cleanup runs (removing a partial copy, `BALANCE_CTL cancel`, `SCRUB_CANCEL`, `DEV_REPLACE cancel`). The record becomes `canceled` only when the handler has returned.

**Interrupt.** A SIGTERM to the host with no `cancel_requested` on a job is a shutdown. For a non-resumable job the context is cancelled and the record is written `interrupted/shutdown`. For a resumable job the host calls the route's `Suspend(ctx, record)` hook with a deadline inside `TimeoutStopSec`; the hook writes `checkpoint` and the record is written `interrupted/shutdown`. Resumable hooks:

| Route | Suspend | Resume |
|---|---|---|
| `storage.btrfs_balance` | `BALANCE_CTL pause`; the kernel keeps the balance item and resumes it at the next mount by itself | attach an observer: `BALANCE_PROGRESS` until the kernel finishes |
| `storage.btrfs_scrub_start` | `SCRUB_CANCEL`; store each device's `last_physical` | restart each device's scrub from its stored offset |
| `storage.btrfs_device_replace` | nothing to do: the kernel cancels on any signal; after a crash the kernel resumes from its item | if the kernel shows the replace running, observe; else start it again with the same source and target |
| `storage.btrfs_device_remove` | nothing: cancelled by the kernel on signal; moved chunks stay moved | issue the remove again |

**Boot.** `linuxio-jobs-reconcile.service` runs after `local-fs.target` and after the pools are mounted (ordered after `linuxio-btrfs-degraded.service`). For every uid directory: records in `running` → `interrupted/reboot` (a power loss leaves them `running`; an orderly shutdown already marked them). Resumable records (always uid 0) are started as workers `linuxio-job@<id>.service`; their outcome is written to the same record. Interrupted non-resumable jobs raise one alert each.

**Worker.** `linuxio-bridge jobs run --id <id>` loads an `interrupted` resumable record and calls its `Resume` hook; `jobs run --from <scheduled-job-id>` creates a new record from a scheduled job's stored request and runs the handler. Either way it writes `running` with `executor.kind=unit`, progress to `/run`, then the terminal record and the alert. `linuxio-job@.service`: root, `Type=exec`, `CollectMode=inactive-or-failed`, `TimeoutStopSec=2min`, the same SIGTERM semantics as the host.

## 12. Updates

- `linuxio.target` does not pull in hosts, workers, the launcher socket or the reconcile service except through ordering. The installer never stops or restarts them; it may `daemon-reload`.
- The hello frame carries a protocol number. Same number: carry on. Older host: the bridge refuses new mutations with error code `update_pending` and message "LinuxIO update waits for N running jobs"; reads work; the UI shows the count on the update banner. The old host exits when idle under its normal rule and the next login starts the new binary.
- Within one release the protocol number only changes when a frame changes shape; adding routes does not change it.

## 13. Alerts

- `failed` and `interrupted` jobs raise an alert through `alerts.Raise` with source `jobs`, key `<id>`, the route, target and error message, and a link to the job in the UI. `canceled` and `completed` do not.
- The alert store is root-owned, so only the root host, the worker and the reconcile service raise alerts; a user host writes the record only. The frontend shows every user "N jobs ended while you were away" from `tasks.list` at login, so non-admins still learn about failures.
- Scheduled scripts use the existing `OnFailure=` hook (`linuxio alert raise`), unchanged.

## 14. Scheduled scripts and system jobs

- Schedules stay as built: a timer and a oneshot service per schedule, `run_now` and `stop` as quick mutations, status from systemd.
- The generated schedule service gains `ExecStopPost=/usr/local/bin/linuxio-bridge jobs record --schedule <id> --result $SERVICE_RESULT --status $EXIT_STATUS`, which writes a job record owned by uid 0 with `executor.kind=unit` and the invocation ID, so runs appear in history.
- The job list includes running unit jobs by reading systemd: `linuxio-schedule-*.service`, `linuxio-job@*.service` and the btrfs units, with the invocation ID as job ID, `ActiveState` as state and the journal as log. This is a read-only adapter in the host's `tasks.list`/`tasks.events`.
- "Scrub monthly" and similar scheduled jobs are timers whose service runs `jobs run --from <id>`, where `<id>` is a scheduled-job record created when the schedule is saved; each firing creates one run record from the stored request. The btrfs snapshot, health and resync units write records the same way so they appear in history. Their behaviour otherwise does not change.

## 15. Security

- Nothing new is reachable from the network. The webserver still relays bytes to a bridge that is the user.
- Authorization is the uid of the connecting process, read by the kernel. The root host accepts only root; a user host accepts only its uid. The launcher starts only the caller's host.
- The record contains the request. It is `0600`, owned by the uid, and no API returns it; snapshots expose `route`, `target`, `owner`, `progress`, `result`, `error` and timestamps, as today.
- The root host runs admin mutations as root, as the root bridge does now. The set of code running as root does not grow: the launcher is about 150 lines and imports no handler.
- A user host runs with `NoNewPrivileges`. The root host too; root does not need setuid binaries. If a handler turns out to need one, the unit drops the flag for uid 0 with a comment saying which.
- Ceilings: 256 running jobs per host; one host per uid; the launcher is idempotent so repeated requests cannot multiply units.
- The 13 root-only routes the map found without `Privileged()` get it during migration.

## 16. Frontend

- `useMutation` everywhere; the three task hooks and `useCallMutation` are deleted after migration, with a codemod for the ~120 call sites.
- The running tray (`frontend/src/tasks/`) keeps its engines for uploads and downloads (bridge-local) and reads everything else from `tasks.events`. It shows running jobs older than one second, and every failed or interrupted one until dismissed.
- A history view lists `tasks.list` for the user: route, target, owner (admins see all owners in the root host), state, duration, error, with a link to the journal filter. Small: one route, one table.
- At login the frontend fetches `tasks.list` once and shows the "ended while you were away" notice when any job finished since the last seen timestamp in UI preferences.
- The update banner shows "waits for N jobs" when a mutation returns `update_pending`.

## 17. Route classification

- `Call`: the 120 reads plus the five cache refreshes the map marks as not real changes (`docker.check_updates`, `docker.check_container_update`, `docker.get_icon`, `docker.get_icon_uri`, `monitoring.check_database`): 125, all `RetrySafe`.
- `Mutation`: the other 194 routes of the map. `tasks.cancel` stays a built-in. The 12 duplex channels are unchanged.
- `BrowserBound`: `filebrowser.upload`, `filebrowser.upload_batch`, `filebrowser.archive`.
- `Resumable`: `storage.btrfs_scrub_start`, `storage.btrfs_balance`, `storage.btrfs_device_replace`, `storage.btrfs_device_remove`. Replication is not resumable: an interrupted run is reported and the hourly timer catches up.
- `Target`: every route that declares `WithTaskMetadata` today keeps its identity as its target. Quick mutations get no target in this design; adding one is a per-route decision later, because a refused double click needs UI handling of its own.

## 18. Bugs fixed during migration

Each is fixed when its route moves, with a test:
1. `storage.btrfs_device_remove`: cancel issues the kernel cancel instead of only marking the task.
2. `storage.btrfs_pool_create`: on failure, undo the partitions it created and wipe the superblocks it wrote.
3. `docker.apply_daemon_storage`: write a checkpoint after stopping Docker so an interrupted run restarts Docker at reconcile rather than leaving it down.
4. The 13 routes missing `Privileged()`.
5. `/etc/default/btrfsmaintenance` and the WireGuard configs: atomic writes.
6. `docs/api-contract.md` route counts and the lifetime section.

## 19. Testing

- **Store**: claim, attach, 409 paths, write policy, pruning, crash detection by pid and start time, transient deletion. Unit tests.
- **Relay and protocol**: a bridge and a host in one test process over a socketpair: start, progress, result, detach, abort, resend attaches, protocol mismatch returns `update_pending`. Unit tests.
- **Host lifecycle**: exit rule with fake clock; SIGTERM marks running jobs and calls `Suspend` within the deadline. Unit tests.
- **Launcher**: fake systemd (`testdbus` exists), peer uid checks, disallowed user, timeout waiting for the socket. Unit tests.
- **Worker and reconcile**: interrupted records are restarted or alerted; a `running` record with a dead pid becomes `interrupted/crash`. Unit tests with the fake kernel for btrfs.
- **btrfs suspend and resume**: added to `make test-btrfs-integration` (root, run by the user): kill a scrub worker and check the pool is consistent; SIGTERM a balance and check it resumes after unmount and mount; scrub resumes from its stored offset; a killed replace is reported and restarted.
- **Browser**: start a copy, log out, log in, find it running in the tray and completed in history (`make test-frontend-browser-quiet`).
- **Performance**: a test that starts a no-op mutation through the relay 1 000 times and asserts a median under 1 ms, so a regression in the relay is caught.
- **Guards**: the inventory test, the `Call` ⇒ `RetrySafe` test, the import boundary test, `logging/style_test.go` for the new log lines.

## 20. Phasing

1. **Skeleton**: store generalisation, host, launcher, relay, units, reconcile service; `docker.update_container` is the first route, since it already has the record shape. The old durable code path is deleted with it. `make test-quiet` green; the launcher and host installed by the installer.
2. **Long work**: the 28 remaining task routes and the 14 long calls move, with the bug fixes of section 18 and the btrfs suspend hooks. The root integration test gains its kill-and-resume steps.
3. **Everything else**: the ~140 quick mutations re-declared as `Mutation`; `Call` reads marked `RetrySafe`; the frontend codemod; the old hooks, `TaskRunner`, lifetimes and policies deleted.
4. **Schedules and system jobs**: `ExecStopPost` record, the unit adapter in the list, scheduled jobs on `linuxio-job@`, btrfs timer units writing records.
5. **History and notices**: the history view, the login notice, the update banner.

Each phase ends with `make test-quiet`, the docs below updated, and a suggested commit message.

## 21. Documentation to update

`docs/api-contract.md` (modes, admission, lifetime, tasks section), `docs/durable-operations-architecture.md` (replaced by this design; keep the fault matrix), `docs/process-systemd-architecture.md` (binaries, units, boot flow), `docs/bridge_handler_patterns.md`, `docs/btrfs.md` (resume semantics), `docs/TODO/scheduled-execution.md`, `docs/frontend-task-state-unification.md`, `docs/TODO/api-reliability-roadmap.md` (phase 4 superseded), and `AGENTS.md` if the handler guidance there changes.

## 22. Measured, not estimated

On the development machine (NVMe, btrfs root), 2026-10-26: relay hop 27 µs median; record write 35 µs without fsync, 1.4 ms with; `StartTransientUnit` to a running process 5 ms on the user manager and 6.3 ms on the system manager; bridge binary start 8 ms warm; `docker stop` of a container that ignores SIGTERM 10.17 s. NAS hardware with a spinning root disk will show a slower fsync, which the write policy already accounts for.
