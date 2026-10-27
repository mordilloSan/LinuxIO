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
| Who starts hosts | `linuxio-auth`, after PAM, for an unprivileged login (`systemctl start --no-block linuxio-jobs@<uid>`); root processes start the root host with `StartUnit`. No socket starts a host. |
| Identity | The kernel's `SO_PEERCRED` on every socket. A claim from a bridge is metadata, never authorization. |
| Same target twice | Refused with 409 while the first runs. No queue, no per-user or per-route cap, no runtime cap. |
| Reboot and power loss | Running jobs become `interrupted`. Resumable ones (btrfs scrub, balance, replace, remove) are restarted by the boot service; the rest alert. |
| Orderly shutdown | Resumable jobs pause and checkpoint on SIGTERM so the restart continues where they were. |
| Updates | Hosts are not part of `linuxio.target` and are never restarted by the installer. An old host finishes its jobs and exits when idle. |
| History | On disk, 30 days or 200 jobs per user. Jobs that succeed in under a second are not kept. |
| Alerts | A failed or interrupted job raises an alert (admin hosts). Success goes to history only. |
| Scheduled scripts | Stay systemd timers and services. They appear in the same list and history and use the same alert hook. |
| Jobs with no user | Timers, the boot service and an admin shell submit jobs to the root host with `linuxio-bridge jobs submit`. One executor, same record, same list. |
| Logs | journald. Host jobs carry a `JOB_ID` field; unit jobs are their own unit. |
| Rejected | A system-wide root daemon running jobs (TrueNAS shape): it would run non-admins' work as root. A process per job started from the bridge: 15 ms each, which forces a fast/slow decision per route. ZFS: out-of-tree module on the user's distro. |

## 3. Terms

- **Bridge**: today's per-login process, as the user. Keeps reads, terminals, uploads and downloads. Relays mutations to the host.
- **Host**: `linuxio-jobs@<uid>.service`. Runs every mutation for that uid as a goroutine. Owns the in-memory job registry for that uid and writes the store.
- **Auth**: `linuxio-auth` starts an unprivileged login's host once PAM has authenticated the user, before the session opens and the bridge spawns.
- **Submit CLI**: `linuxio-bridge jobs submit`, the local client that starts a job in a host without a browser: timers, the boot service, an admin shell, tests. As root it starts the root host itself; as a user it connects to the host its login started.
- **Store**: `/var/lib/linuxio/jobs/<uid>/`, one JSON record per job, owned by the uid.
- **Job**: a record plus a handler run in a host. Started by a user through the bridge, or by the system through the submit CLI. The host is the only executor.
- **Target**: what a job works on (pool UUID, disk, container, compose project, set of paths), declared by the route.

## 4. Architecture

```
browser ──WebSocket── linuxio-webserver (unprivileged) ──yamux── bridge (per login, as the user)
                                                                     │  reads, terminals,
                                                                     │  upload/download streams
                                                                     │
                                            every mutation stream, relayed unchanged
                                                                     ▼
   linuxio-auth (root, after PAM) ──systemctl start──▶ systemd ──▶ linuxio-jobs@<uid>.service
        │  for an unprivileged login, before the bridge              host, as <uid>
        │                                                         ├─ job registry (memory)
   bridge, once per login                                         ├─ handlers (shared code)
                                                                  └─ store /var/lib/linuxio/jobs/<uid>/
                                                                       live progress /run/linuxio/jobs/<uid>/

   systemd timers, boot service, admin shell ──jobs submit──▶ linuxio-jobs@0.service
                                   (StartUnit as root, then the same socket, same host, same record)
```

The webserver, `linuxio-auth` and the bridge keep their roles and boundaries.
Nothing new listens on the network and no new socket is reachable by other users. The one new root piece is the root host, which runs admin mutations as root exactly as the root bridge does today; `linuxio-auth` gains one bounded `systemctl start`.

## 5. Programming model

### Backend

The handler comes first and Go infers the request and result types from it, so
no declaration spells out type parameters:

```go
// A read: retry-safe by definition, 30 s client timeout, cached by TanStack Query.
apischema.Call("docker.list_containers", listContainers)

// A mutation. No fast/slow decision.
apischema.Mutation("docker.start_container", startContainer)

// A mutation that resumes after reboot. Progress is reported through ctx,
// so the handler signature is the same as above. Its target (the pool) comes
// from the request type, not from the declaration.
apischema.Mutation("storage.btrfs_scrub_start", runScrub, apischema.Privileged()).
    Resumable(scrubResume)

// In apischema, next to the type: every route that takes a UUIDRequest
// works on that pool, so the host refuses a second run on the same pool.
func (r UUIDRequest) Target() []string { return []string{r.UUID} }
```

- `Call(name, fn, opts...)` and `Mutation(name, fn, opts...)` take `func(ctx, Req) (Res, error)`; `MutationVoid` takes `func(ctx, Req) error` for the 61 handlers with no result. Today's two-argument handlers keep their signatures.
- Progress: `bridgeipc.ReportProgress(ctx, TaskProgress{...})`. The host puts a reporter in the context; outside a job the call is a no-op. The ~30 handlers that take a `report` argument today drop it and call this instead. One handler signature, no `Run` form.
- **Target** is a method on the request type, `Target() []string`, naming what the job works on: a pool UUID, a device, a container, a compose project, the paths of a copy. The binding detects it once at startup; the host uses it for the same-target refusal (section 11) and the job list shows it. A request type without the method has no target and is never refused as a duplicate. This replaces today's `WithTaskMetadata` closures on every route with one method per request type.
- `Resumable(hooks)` is a method on the binding because the hooks are typed by the request: `Suspend(ctx, record) (checkpoint, error)` and `Resume(ctx, record) (Res, error)` (section 11). It is valid only with `Privileged()`.
- Options that need no type (`Privileged()`, `BrowserBound()`, `NoEndpoint()`) stay plain options.
- `RetrySafe` is no longer declared: a `Call` is a read and is retry-safe by definition.
- `TaskRunner`, `SessionTask`, `DurableTask`, `WithTaskLifetime`, `WithTaskIdentity`, `WithTaskProgress` and the `TaskPolicy` admission are removed. The progress detail type of a route is declared once as `apischema.Progress[Detail]()` so the generator still types `onProgress` per route.
- Three routes keep the bridge: `filebrowser.upload`, `filebrowser.upload_batch`, `filebrowser.archive`. They are declared `Mutation` with `apischema.BrowserBound()`, which the generator and the relay use to keep them local. This is the only exception and it is mechanical: the bytes come from or go to the browser.

### Guard tests

- A `Call` is retried after connection loss by construction, so the eight reads not marked `RetrySafe` today (`docker.get_icon`, `docker.get_icon_uri`, `docker.check_updates`, `docker.check_container_update`, `monitoring.check_database`, `system.get_health_summary`, `network.get_network_info`, `terminal.list_shells`) are reviewed once for a side effect a repeat could double; none is expected, since they only refresh caches.
- Every `Mutation` appears in a checked inventory table with its `Resumable` and `BrowserBound` flags, the way `handleEventsInventory` worked. Adding a mutation without a row fails the test.

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

- At startup the bridge connects to `/run/linuxio/jobs/<uid>/host.sock`, retrying for 5 s while the host comes up, and keeps one yamux session for its lifetime. `linuxio-auth` started a user's host before spawning the bridge; a privileged bridge starts the root host itself (section 9). If the connect fails it retries with backoff and reports mutations as `connection_unavailable` meanwhile. Measured cost per mutation on the development machine: 27 µs median, 55 µs p99.
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

- **Start**: by `linuxio-auth` when an unprivileged user logs in (`systemctl start --no-block linuxio-jobs@<uid>.service` once PAM has authenticated the user, before the session opens and the bridge is spawned; a failure is logged and the login proceeds), or by any root process that wants to submit a job (a timer's service, the boot reconcile service, an admin shell) with `StartUnit` on `linuxio-jobs@0.service`, which the submit CLI does itself. Starting a running host is a no-op. `StateDirectory` and `RuntimeDirectory` create the store and socket directories owned by the uid; no custom code creates them.
- **Stay up while** connected bridges > 0 or running jobs > 0.
- **Exit** when both are zero for one minute (so a logout and login within seconds does not restart it). Exit is `sd_notify(STOPPING=1)` then process exit; systemd collects the unit.
- **Stop by systemd** (shutdown, or `systemctl stop`): SIGTERM. The host marks every running job: resumable ones get up to `TimeoutStopSec` to suspend (section 11), the rest are marked `interrupted` with reason `shutdown`. Then it exits.
- **Crash**: the next host for that uid, or the boot service, finds records in `running` whose executor pid and start time are not alive and marks them `interrupted` with reason `crash`.
- **Limits**: a host refuses a new job above 256 running jobs for its uid with 409. This is a safety valve against a runaway client, not a UX limit.

## 9. Who starts a host

`linuxio-auth` starts an unprivileged login's host. Once PAM has authenticated the user and the sudoers probe has decided the login is unprivileged, it runs `/usr/bin/systemctl start --no-block --quiet linuxio-jobs@<uid>.service` as root in a child with a cleared environment, bounded by 3 s and the login's request deadline. It does so before the PAM session opens, because `pam_selinux` gives later children the user's exec context; systemctl runs as the auth worker, like the sudo probe. The bridge is spawned after the session opens. The uid is the one PAM resolved; nothing from the wire reaches the command. A failure is a journal entry with the user and the unit; the login proceeds and the bridge answers mutations with 503 until the next login starts the host.

A privileged bridge runs as root and calls `StartUnit` on `linuxio-jobs@0.service` itself, as do the submit CLI run as root and the boot reconcile. Starting a running unit is a no-op.

Nothing else starts a host: there is no socket a local process can use to request one, so a user's host exists only after that user authenticated through LinuxIO, or when root starts it. The host unit sets `TasksMax=8192`, room for 256 jobs each waiting on a subprocess tree, and `MemoryMax=2G`, with `OOMPolicy=continue`, so an OOM kill of one job's subprocess fails that job instead of stopping the host.

**Open before phase 2.** A user's host that exits while that user is logged in is not started again until their next login: a crash, an OOM kill of the host process itself, a queued start that fails, or the idle exit racing a new login whose bridge has not joined yet. No phase 1 route reaches a user's host, so nothing is lost today, but the routes phase 2 moves for non-admins need this closed first. The candidate fix is a per-uid activation socket, `linuxio-jobs@<uid>.socket`, owned by the uid with mode `0600`, which `linuxio-auth` starts instead of the service: a dial then starts the host whenever it is down, and the idle exit cannot race a login.

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

Retention: terminal records are kept 30 days and at most 200 per uid; pruning runs at host start and daily, removing artifacts with records. The job store, `backend/common/jobs`, implements this policy; phase 1 prunes at host start only.

## 11. Job lifecycle

**Claim.** The host validates the request with the route decoder, builds the target, and checks the `id`: unknown → new job; known with the same route and request → attach; known with a different route or request → 409. Then admission: a running job of the same route with the same target → 409 `"<route> is already running on this target"`. Admission is per host, which is per uid; for admins the root host is shared, so privileged targets (pools, disks, containers, Docker, packages) are checked system-wide.

**Run.** The handler runs in a goroutine with a context cancelled only by `tasks.cancel`, stream abort, or host stop. `report` writes a progress frame to every attached stream and to the `/run` file (coalesced). The handler's log lines carry `JOB_ID=<id>`, `JOB_ROUTE=<route>` as journald fields through the existing slog handler.

**Finish.** The result frame goes to attached streams; the record is written `completed` or `failed`; a failed job raises an alert (section 13). A `completed` job younger than one second is dropped from the store.

**Cancel.** `tasks.cancel` or an abort sets `cancel_requested` in the record *first*, then cancels the context. The handler's own cleanup runs (removing a partial copy, `BALANCE_CTL cancel`, `SCRUB_CANCEL`, `DEV_REPLACE cancel`). The record becomes `canceled` only when the handler has returned.

**Interrupt.** A SIGTERM to the host with no `cancel_requested` on a job is a shutdown. For a non-resumable job the context is cancelled and the record is written `interrupted/shutdown`. For a resumable job the host calls the route's `Suspend(ctx, record)` hook with a deadline inside `TimeoutStopSec`; the hook returns the `checkpoint` and the record is written `interrupted/shutdown`. The contract is generic: any route may declare the two hooks, and the host, the store and the reconcile service know nothing about what they do. The first four resumable routes are the btrfs operations, because the kernel ties them to a process:

| Route | Suspend | Resume |
|---|---|---|
| `storage.btrfs_balance` | `BALANCE_CTL pause`; the kernel keeps the balance item and resumes it at the next mount by itself | attach an observer: `BALANCE_PROGRESS` until the kernel finishes |
| `storage.btrfs_scrub_start` | `SCRUB_CANCEL`; store each device's `last_physical` | restart each device's scrub from its stored offset |
| `storage.btrfs_device_replace` | nothing to do: the kernel cancels on any signal; after a crash the kernel resumes from its item | if the kernel shows the replace running, observe; else start it again with the same source and target |
| `storage.btrfs_device_remove` | nothing: cancelled by the kernel on signal; moved chunks stay moved | issue the remove again |

**Boot.** `linuxio-jobs-reconcile.service` runs after `local-fs.target` and after the pools are mounted (ordered after `linuxio-btrfs-degraded.service`). For every uid directory: records in `running` → `interrupted/reboot` (a power loss leaves them `running`; an orderly shutdown already marked them). For each `interrupted` resumable record (always uid 0) it runs `jobs submit --resume <id>`, which starts the root host if needed and asks it to run the route's `Resume` hook on that record; the outcome is written to the same record. Interrupted non-resumable jobs raise one alert each. The host then exits under its normal rule once the resumed jobs end.

**Submit CLI.** `linuxio-bridge jobs submit --route <route> --request <json> [--wait]` creates a job in the caller's host exactly as a bridge would: as root it calls `StartUnit` on `linuxio-jobs@0.service` and connects to its socket; as a user it connects to the host its login started, so it works only while that user has a LinuxIO login or the host is still up. `--resume <id>` resumes an interrupted resumable record; `--from <scheduled-job-id>` creates a run record from a scheduled job's stored request. `--wait` follows the job and exits with its outcome, so a timer's oneshot service reflects the job's success or failure. There is no second executor: a job submitted by a timer runs in the same host, with the same progress, record, log and alert as one started from the browser.

### Fault matrix

What each event does to a job, as phase 1 implements it:

- **Page reload, tab close, connection loss**: the stream detaches and the job runs on. Lost before the `task.snapshot` frame, the client resends the same envelope once (it attaches, or gets the stored outcome); lost after it, the client reattaches with `tasks.watch`. A second loss is `outcome_unknown`.
- **Resend of an `id` with another route or request**: 409.
- **Logout, session expiry, bridge exit**: the bridge closes its host connection; jobs run on. The host exits one minute after its last bridge left and its last job ended. A later login of the uid sees the jobs the host still holds through `tasks.list` and `tasks.events`.
- **Host unreachable from the bridge**: mutations fail with 503 before anything starts; the bridge reconnects with backoff from 1 s to 30 s.
- **Host on another protocol (an update in between)**: new mutations fail with 409 `update_pending`; reads work; the old host finishes its jobs.
- **LinuxIO update**: hosts and the reconcile service are outside `linuxio.target`; the installer never stops them.
- **`systemctl stop` or orderly shutdown**: running jobs become `interrupted/shutdown`, resumable ones after `Suspend` stored a checkpoint; the root host alerts.
- **Host crash or kill**: the next start of that uid's host marks its `running` records whose pid and start time are gone `interrupted/crash`; the root host alerts.
- **Reboot or power loss**: the boot reconcile marks every uid's `running` records `interrupted/reboot`, resumes interrupted resumable root jobs, and alerts on the non-resumable root jobs it marked.
- **Cancel** (`tasks.cancel`, stream abort): the job's context is cancelled and its record becomes `canceled` at once; the target stays taken until the handler returns. Phase 1 does not write `cancel_requested`.

## 12. Updates

- `linuxio.target` does not pull in hosts or the reconcile service except through ordering. The installer never stops or restarts them; it may `daemon-reload`.
- The hello frame carries a protocol number. Same number: carry on. Older host: the bridge refuses new mutations with error code `update_pending` and message "LinuxIO update waits for N running jobs"; reads work; the UI shows the count on the update banner. The old host exits when idle under its normal rule and the next login starts the new binary.
- Within one release the protocol number only changes when a frame changes shape; adding routes does not change it.

## 13. Alerts

- `failed` and `interrupted` jobs raise an alert through `alerts.Raise` with source `jobs`, key `<id>`, the route, target and error message, and a link to the job in the UI. `canceled` and `completed` do not.
- The alert store is root-owned, so only the root host and the reconcile service raise alerts; a user host writes the record only. The frontend shows every user "N jobs ended while you were away" from `tasks.list` at login, so non-admins still learn about failures.
- Scheduled scripts use the existing `OnFailure=` hook (`linuxio alert raise`), unchanged.

## 14. Scheduled scripts and system jobs

- Schedules stay as built: a timer and a oneshot service per schedule, `run_now` and `stop` as quick mutations, status from systemd.
- The generated schedule service gains `ExecStopPost=/usr/local/bin/linuxio-bridge jobs record --schedule <id> --result $SERVICE_RESULT --status $EXIT_STATUS`, which writes a job record owned by uid 0 with `executor.kind=unit` and the invocation ID, so runs appear in history.
- The job list includes running scripts by reading systemd: `linuxio-schedule-*.service` and the btrfs timer units, with the invocation ID as job ID, `ActiveState` as state and the journal as log. This is a read-only adapter in the host's `tasks.list`/`tasks.events`; scripts are the one kind of work that does not run in a host, because they are arbitrary commands under a configured user.
- "Scrub monthly" and similar scheduled jobs are timers whose oneshot service runs `linuxio-bridge jobs submit --from <id> --wait`, where `<id>` is a scheduled-job record created when the schedule is saved; each firing creates one run record in the root host from the stored request. The btrfs snapshot, health and resync units keep running their own code and gain the `ExecStopPost` record so they appear in history; moving them onto `jobs submit` is a later, optional step.

## 15. Security

- Nothing new is reachable from the network. The webserver still relays bytes to a bridge that is the user.
- Authorization is the uid of the connecting process, read by the kernel. The root host accepts only root; a user host accepts only its uid. `linuxio-auth` starts a host only for the uid PAM admitted.
- The record contains the request. It is `0600`, owned by the uid, and no API returns it; snapshots expose `route`, `target`, `owner`, `progress`, `result`, `error` and timestamps, as today.
- The root host runs admin mutations as root, as the root bridge does now. The set of code running as root grows by one bounded `systemctl start` in `linuxio-auth`.
- A user host runs with `NoNewPrivileges`. The root host too; root does not need setuid binaries. If a handler turns out to need one, the unit drops the flag for uid 0 with a comment saying which.
- Ceilings: 256 running jobs per host; one host per uid; starting a running unit is a no-op, so repeated logins cannot multiply hosts.
- The 13 root-only routes the map found without `Privileged()` get it during migration.

## 16. Frontend

- `useMutation` everywhere; the three task hooks and `useCallMutation` are deleted after migration, with a codemod for the ~120 call sites.
- The running tray (`frontend/src/tasks/`) keeps its engines for uploads and downloads (bridge-local) and reads everything else from `tasks.events`. It shows running jobs older than one second, and every failed or interrupted one until dismissed.
- A history view lists `tasks.list` for the user: route, target, owner (admins see all owners in the root host), state, duration, error, with a link to the journal filter. Small: one route, one table.
- At login the frontend fetches `tasks.list` once and shows the "ended while you were away" notice when any job finished since the last seen timestamp in UI preferences.
- The update banner shows "waits for N jobs" when a mutation returns `update_pending`.

## 17. Route classification

- `Call`: the 115 reads plus the five cache refreshes the map marks as not real changes (`docker.check_updates`, `docker.check_container_update`, `docker.get_icon`, `docker.get_icon_uri`, `monitoring.check_database`): 120, all `RetrySafe`.
- `Mutation`: the other 199 routes of the map. `tasks.cancel` stays a built-in. The 12 duplex channels are unchanged. Total 332.
- `BrowserBound`: `filebrowser.upload`, `filebrowser.upload_batch`, `filebrowser.archive`.
- `Resumable`: `storage.btrfs_scrub_start`, `storage.btrfs_balance`, `storage.btrfs_device_replace`, `storage.btrfs_device_remove`. Replication is not resumable: an interrupted run is reported and the hourly timer catches up.
- `Target()`: the request types behind the routes that declare `WithTaskMetadata` today implement it with the same identity. Request types of quick mutations do not implement it in this design; adding one is a per-type decision later, because a refused double click needs UI handling of its own.

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
- **Auth**: the hermetic PAM suite asserts the exact `systemctl` arguments of an unprivileged login and that a failed start keeps the login.
- **Submit CLI and reconcile**: `jobs submit` against a host in the test process; interrupted records are resumed or alerted; a `running` record with a dead pid becomes `interrupted/crash`. Unit tests with the fake kernel for btrfs.
- **btrfs suspend and resume**: added to `make test-btrfs-integration` (root, run by the user): kill the host mid-scrub and check the pool is consistent; SIGTERM a balance and check it resumes after unmount and mount; scrub resumes from its stored offset; a killed replace is reported and restarted.
- **Browser**: start a copy, log out, log in, find it running in the tray and completed in history (`make test-frontend-browser-quiet`).
- **Performance**: a test that starts a no-op mutation through the relay 1 000 times and asserts a median under 1 ms, so a regression in the relay is caught.
- **Guards**: the inventory test, the `Call` ⇒ `RetrySafe` test, the import boundary test, `logging/style_test.go` for the new log lines.

## 20. Phasing

1. **Skeleton**: store generalisation, host, relay, units, reconcile service; `docker.update_container` is the first route, since it already has the record shape. The old durable code path is deleted with it. `make test-quiet` green; the host installed by the installer.
2. **Long work**: the 28 remaining task routes and the 14 long calls move, with the bug fixes of section 18 and the btrfs suspend hooks. The root integration test gains its kill-and-resume steps.
3. **Everything else**: the ~140 quick mutations re-declared as `Mutation`; `Call` reads marked `RetrySafe`; the frontend codemod; the old hooks, `TaskRunner`, lifetimes and policies deleted.
4. **Schedules and system jobs**: `ExecStopPost` record, the unit adapter in the list, scheduled jobs through `jobs submit --from`, btrfs timer units writing records.
5. **History and notices**: the history view, the login notice, the update banner.

Each phase ends with `make test-quiet`, the docs below updated, and a suggested commit message.

## 21. Documentation to update

`docs/api-contract.md` (modes, admission, lifetime, tasks section), the durable-operations architecture document (deleted; its fault matrix is in section 11), `docs/process-systemd-architecture.md` (binaries, units, boot flow), `docs/bridge_handler_patterns.md`, `docs/btrfs.md` (resume semantics), `docs/TODO/scheduled-execution.md`, `docs/frontend-task-state-unification.md`, `docs/TODO/api-reliability-roadmap.md` (phase 4 superseded), and `AGENTS.md` if the handler guidance there changes.

## 22. Measured, not estimated

On the development machine (NVMe, btrfs root), 2026-10-26: relay hop 27 µs median; record write 35 µs without fsync, 1.4 ms with; `StartTransientUnit` to a running process 5 ms on the user manager and 6.3 ms on the system manager; bridge binary start 8 ms warm; `docker stop` of a container that ignores SIGTERM 10.17 s. NAS hardware with a spinning root disk will show a slower fsync, which the write policy already accounts for.
