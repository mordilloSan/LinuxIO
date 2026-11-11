# Jobs: mutations that outlive the login

## Status

This is the one document for the jobs work: the design, the plan for every
phase, what is done, what is left, and every item deferred by the reviews. It
replaces the jobs design spec, the phase 1 implementation plan and the local
ledger kept while phase 1 ran. Their history is in git:
`git log -- '*jobs-design.md' '*jobs-phase-1.md'`. The phase 3 plan went the
same way once phase 3 was done: `git log -- '*jobs-phase3-storage-safety.md'`.
The route classification it relies on stays in the
[mutation map](../superpowers/specs/2026-10-25-mutation-map.md).

Keep this document until phase 5 is done, then fold the verified behaviour into
the focused documents and delete it. What already shipped is described there
too: [API Contract](../api-contract.md#tasks-and-jobs),
[Process & Systemd Architecture](../process-systemd-architecture.md#login-and-jobs)
and [Notifications](../alerts.md).

| Phase | Scope | State |
|---|---|---|
| 1 | Store, host, relay, units, first route (`docker.update_container`) | done, verified on a real host |
| 2 | Every mutation runs as a job: prerequisites, the ~199 routes and their callers, old task code deleted | done (section 4.2), last step `d2871872`; checked in the test VM (section 3.5) |
| 3 | Storage safety and btrfs resume: cancel that ends when the handler returns, resume hooks for four btrfs routes, rollbacks, Docker storage switch, kill-and-resume root tests | done (section 4.3), commits `938e32c5` and `f8db05af`; checked in the test VM (section 3.6). The jobs CLI verb and the host's resume call are removed (D6) |
| 4 | Scheduled runs in the job list and the store | done (section 4.4); checked in the test VM (section 3.8); [plan](../superpowers/plans/2026-11-10-jobs-phase4-scheduled-runs.md) |
| 5 | History view, update banner; configurable notifications and the notice at sign-in | 5a built: the history page, each schedule's past runs, the saved output viewer and the update banner; checked in the test VM, where run 3 passed all 7 checks and run 4, with the needrestart check added, all 8; of the three faults the VM found, the history page's empty table and the viewer's opening position are fixed and confirmed there, and needrestart restarting job hosts is fixed by an override the installer ships, checked there too (section 3.9, finding 2). Open with the owner: the bell and cancel while an update waits (section 3.9, limits); the history then says it is unavailable (decided on 2026-11-10, section 3.9); [plan](../superpowers/plans/2026-11-10-jobs-phase5a-history-and-banner.md). 5b built, not yet committed: what each kind of work reports, the grid of level by channel, a schedule's own choice and the notice at sign-in; checked in the test VM, where run 4, after the final fix round, passed all 11 checks (section 3.10). Open with the owner: four update routes without `Privileged()`, the Go 1.27.4 pin, two alert gaps that predate it (section 3.10); [plan](../superpowers/plans/2026-11-10-jobs-phase5b-notifications.md) |

Contents: 1. Summary · 2. Design · 3. Done · 4. Left to do · 5. Deferred
items · 6. Decision log · 7. Host-event checks

## 1. Summary

A mutation started from the UI runs as a job in a per-uid job host,
`linuxio-jobs@<uid>.service`, the `linuxio-bridge` binary started by systemd
with no arguments when a connection reaches its socket,
`linuxio-jobs@<uid>.socket`. The per-login bridge relays each mutation stream
to it, so a job survives logout, a closed tab, a bridge crash and a LinuxIO
update. `linuxio-auth` starts a non-admin's socket at login, after PAM; root
starts uid 0's, and the root host also starts at boot. Each job is a JSON
record in `/var/lib/linuxio/jobs/<uid>/`. A host marks the jobs its
predecessor left running as interrupted when it starts, and the root host
resumes the resumable ones the last shutdown or reboot interrupted (section 2.11).

## 2. Design

### 2.1 Goal

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

### 2.2 Decisions in one page

| Decision | Choice |
|---|---|
| API shape | `Call` means read. Every write is a `Mutation`. One stream carries request, progress and result. One round trip. |
| Client timeout | None for mutations. Connection loss never cancels a mutation. Reads keep the 30 s timeout and the retry policy. |
| Where mutations run | In a **job host**: one long-lived process per uid, as that uid (root for admins), started at login, exiting when no bridge is connected and no job runs. |
| Who starts hosts | A connection to the host's socket, `linuxio-jobs@<uid>.socket`, which systemd holds. `linuxio-auth` starts a user's socket after PAM; root processes start uid 0's, and the root host is enabled at boot. Only that uid can connect. |
| Identity | The kernel's `SO_PEERCRED` on every socket. A claim from a bridge is metadata, never authorization. |
| Same target twice | Refused with 409 while the first runs. No queue, no per-user or per-route cap, no runtime cap. |
| Reboot and power loss | Running jobs become `interrupted`. The root host starts at boot, resumes the resumable ones the shutdown or power loss interrupted (btrfs scrub, balance, replace, remove) and alerts on the rest. Nothing is resumed later than that boot. |
| Orderly shutdown | Resumable jobs pause and checkpoint on SIGTERM so the restart continues where they were. |
| Updates | Hosts are not part of `linuxio.target` and are never restarted by the installer. An old host finishes its jobs and exits when idle. |
| History | On disk, 30 days or 200 jobs per user. Jobs that succeed in under a second are not kept. |
| Alerts | A failed job raises an alert, and an interrupted one an alert or a warning by its cause: a crash is an alert, a shutdown or a reboot a warning (root host only). A job that succeeds goes to history only, unless the notification settings ask for long jobs (`browser_jobs: long_jobs`) or for every run of a script (`every_run`), which raise a notice (plan 5b). |
| Scheduled scripts | Stay systemd timers and services. They appear in the same list and history and use the same alert hook. |
| Jobs with no user | Nothing outside the browser starts a job. Scheduled work runs in its own systemd unit and writes its own record (D8, section 2.14). Same record, same list. |
| Logs | journald. Host jobs carry a `JOB_ID` field; unit jobs are their own unit. |
| Rejected | A system-wide root daemon running jobs (TrueNAS shape): it would run non-admins' work as root. A process per job started from the bridge: 15 ms each, which forces a fast/slow decision per route. ZFS: out-of-tree module on the user's distro. |

### 2.3 Terms

- **Bridge**: today's per-login process, as the user. Keeps reads, terminals, uploads and downloads. Relays mutations to the host.
- **Host**: `linuxio-jobs@<uid>.service`. Runs every mutation for that uid as a goroutine. Owns the in-memory job registry for that uid and writes the store.
- **Auth**: `linuxio-auth` starts an unprivileged login's host once PAM has authenticated the user, before the session opens and the bridge spawns.
- **Store**: `/var/lib/linuxio/jobs/<uid>/`, one JSON record per job, owned by the uid.
- **Job**: a record plus a handler run in a host. Started by a user through the bridge. The host is the only executor.
- **Target**: what a job works on (pool UUID, disk, container, compose project, set of paths), declared by the route.

### 2.4 Architecture

```
browser ──WebSocket── linuxio-webserver (unprivileged) ──yamux── bridge (per login, as the user)
                                                                     │  reads, terminals,
                                                                     │  upload/download streams
                                                                     │
                                            every mutation stream, relayed unchanged
                                                                     ▼
   linuxio-auth (root, after PAM) ──systemctl start──▶ systemd ──▶ linuxio-jobs@<uid>.service
        │  its socket at login; a connection starts the host         host, as <uid>
        │                                                         ├─ job registry (memory)
   bridge, once per login                                         ├─ handlers (shared code)
                                                                  └─ store /var/lib/linuxio/jobs/<uid>/
                                                                       live progress /run/linuxio/jobs/<uid>/
```

The webserver, `linuxio-auth` and the bridge keep their roles and boundaries.
Nothing new listens on the network and no new socket is reachable by other users. The one new root piece is the root host, which runs admin mutations as root exactly as the root bridge does today; `linuxio-auth` gains one bounded `systemctl start`.

### 2.5 Programming model

#### Backend

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
- **Target** is a method on the request type, `Target() []string`, naming what the job works on: a pool UUID, a device, a container, a compose project, the paths of a copy. The binding detects it once at startup; the host uses it for the same-target refusal (section 2.11) and the job list shows it. A request type without the method has no target and is never refused as a duplicate. This replaces today's `WithTaskMetadata` closures on every route with one method per request type.
- `Resumable(hooks)` is a method on the binding because the hooks are typed by the request: `Suspend(ctx, record) (checkpoint, error)` and `Resume(ctx, record) (Res, error)` (section 2.11). It is valid only with `Privileged()`.
- Options that need no type (`Privileged()`, `BrowserBound()`, `NoEndpoint()`) stay plain options.
- `RetrySafe` is no longer declared: a `Call` is a read and is retry-safe by definition.
- `TaskRunner`, `SessionTask`, `DurableTask`, `WithTaskLifetime`, `WithTaskIdentity`, `WithTaskProgress` and the `TaskPolicy` admission are removed. The progress detail type of a route is declared once as `apischema.Progress[Detail]()` so the generator still types `onProgress` per route.
- Three routes keep the bridge: `filebrowser.upload`, `filebrowser.upload_batch`, `filebrowser.archive`. They are declared `Mutation` with `apischema.BrowserBound()`, which the generator and the relay use to keep them local. This is the only exception and it is mechanical: the bytes come from or go to the browser.

#### Guard tests

- A `Call` is retried after connection loss by construction, so the eight reads not marked `RetrySafe` today (`docker.get_icon`, `docker.get_icon_uri`, `docker.check_updates`, `docker.check_container_update`, `monitoring.check_database`, `system.get_health_summary`, `network.get_network_info`, `terminal.list_shells`) are reviewed once for a side effect a repeat could double; none is expected, since they only refresh caches.
- Every `Mutation` appears in a checked inventory table with its `Resumable` and `BrowserBound` flags, the way `handleEventsInventory` worked. Adding a mutation without a row fails the test.

#### Frontend

One generated hook per mutation, `linuxio.<domain>.<name>.useMutation(config)`, with the `ActionConfig` of today's `useCallMutation` plus optional `onProgress(progress, snapshot)` and `onStart(snapshot)`. A promise form `linuxio.<domain>.<name>(request, { onProgress, signal })` exists for non-component code. `useCallMutation`, `useTaskAction` and `useTaskStreamAction` are removed. Query invalidation on success keeps using `operation-query-invalidations.ts`.

### 2.6 Wire protocol

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

### 2.7 Bridge to host relay

- At startup the bridge connects to `/run/linuxio/jobs/<uid>.sock`, retrying for 5 s while the socket comes up, and keeps one yamux session for its lifetime; the connection starts the host if it is down. `linuxio-auth` started a user's socket before spawning the bridge; a privileged bridge starts the root host's socket itself (section 2.9). If the connect fails it retries with backoff and reports mutations as `connection_unavailable` meanwhile. Measured cost per mutation on the development machine: 27 µs median, 55 µs p99.
- The first stream is a hello: `{protocol: N, session: {username, uid, privileged, session_ref}}`. The host checks the peer uid with `SO_PEERCRED`: a user host accepts only its own uid; the root host accepts only uid 0. The session facts are recorded as the job's owner for listing, history and alerts; they are not authorization.
- Each mutation opens one stream; the bridge forwards frames in both directions unchanged and closes when either side closes. Abort forwards as abort.
- The bridge serves itself: all `Call` routes, duplex channels (terminals, log follows), the three browser-bound routes and their `tasks.data`.
- Session end does not cancel anything in the host. The bridge closes its yamux session; the host counts one connected bridge fewer.

### 2.8 Host lifecycle

Unit template `linuxio-jobs@.service`, instance = uid:

```ini
[Unit]
Description=LinuxIO jobs for uid %i
Wants=linuxio-jobs@%i.socket
After=linuxio-jobs@%i.socket local-fs.target linuxio-btrfs-degraded.service
# Deliberately not PartOf=linuxio.target: updates must not stop running jobs.

[Service]
Type=notify
User=%i
# No arguments: the bridge binary is the host when systemd passes it the socket.
ExecStart=/usr/local/bin/linuxio-bridge
StateDirectory=linuxio/jobs/%i
RuntimeDirectory=linuxio/jobs/%i
RuntimeDirectoryPreserve=yes
TimeoutStopSec=2min
KillMode=mixed
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
```

Socket template `linuxio-jobs@.socket`, instance = uid:

```ini
[Socket]
ListenStream=/run/linuxio/jobs/%i.sock
FileDescriptorName=linuxio-jobs
SocketUser=%i
SocketMode=0600
Accept=no
```

- **Start**: by a connection to its socket, which systemd holds. `linuxio-auth` starts a user's socket when they log in (section 2.9); root processes start uid 0's with `StartUnit` on `linuxio-jobs@0.socket`, and `linuxio-jobs@0.service` is enabled so the root host also starts at boot. Starting a listening socket or a running host is a no-op. `StateDirectory` and `RuntimeDirectory` create the store and progress directories owned by the uid; no custom code creates them.
- **Stay up while** connected bridges > 0 or running jobs > 0.
- **Exit** when both are zero for one minute (so a logout and login within seconds does not restart it). Exit is `sd_notify(STOPPING=1)` then process exit; systemd collects the unit.
- **Stop by systemd** (shutdown, or `systemctl stop`): SIGTERM. The host marks every running job: resumable ones get up to `TimeoutStopSec` to suspend (section 2.11), the rest are marked `interrupted` with reason `shutdown`. Then it exits.
- **Crash**: the next host for that uid, started by the next connection, finds records in `running` whose executor pid and start time are not alive and marks them `interrupted` with reason `crash`; those whose executor ran in an earlier boot get reason `reboot` (section 2.11).
- **Limits**: a host refuses a new job above 256 running jobs for its uid with 409. This is a safety valve against a runaway client, not a UX limit.

### 2.9 Who starts a host

A connection to its socket. `linuxio-jobs@<uid>.socket` listens on `/run/linuxio/jobs/<uid>.sock`, owned by the uid with mode `0600`, so only that uid and root can connect. systemd holds the socket and starts `linuxio-jobs@<uid>.service` whenever a connection finds the host down: after an idle exit, a crash, an OOM kill of the host process, or an update that replaced its binary. A connection that arrives while the host is exiting waits in the socket's queue and starts the next host, so the idle exit cannot strand a login. A host that cannot start closes the connections that were waiting, so the clients' backoff paces the next attempt instead of systemd's start limit failing the socket.

`linuxio-auth` starts an unprivileged login's socket. Once PAM has authenticated the user and the sudoers probe has decided the login is unprivileged, it runs `/usr/bin/systemctl start --no-block --quiet linuxio-jobs@<uid>.socket` as root in a child with a cleared environment, bounded by 3 s and the login's request deadline. It does so before the PAM session opens, because `pam_selinux` gives later children the user's exec context; systemctl runs as the auth worker, like the sudo probe. The bridge is spawned after the session opens. The uid is the one PAM resolved; nothing from the wire reaches the command. A failure is a warning in the journal with the user and the unit; the login proceeds and the bridge answers mutations with 503 until the next login.

A privileged bridge runs as root and calls `StartUnit` on `linuxio-jobs@0.socket` itself. A socket stays until reboot, so a user's socket exists only after that user authenticated through LinuxIO, or when root starts it.

As the host, the bridge binary takes no arguments: it is the job host when systemd passes it the socket named `linuxio-jobs` (`FileDescriptorName=`), and refuses to run without it.

The host unit sets `TasksMax=8192`, room for 256 jobs each waiting on a subprocess tree, and `MemoryMax=2G`, with `OOMPolicy=continue`, so an OOM kill of one job's subprocess fails that job instead of stopping the host.

### 2.10 Store and records

Layout per uid, created by systemd:

```
/var/lib/linuxio/jobs/<uid>/<id>.json         record, 0600
/var/lib/linuxio/jobs/<uid>/<id>.result       bounded result artifact, 0600, when the result exceeds 64 KB
/run/linuxio/jobs/<uid>/<id>.progress         live progress, memory-backed, rewritten at most twice a second
/run/linuxio/jobs/<uid>.sock                  host socket, 0600, owned by the uid, held by systemd
```

Record fields: `id`, `route`, `owner {uid, username}`, `target []string`, `state`, `reason` (for `interrupted`: `shutdown`, `crash`, `reboot`), `created_at`, `started_at`, `finished_at`, `request` (the full decoded request, needed to run or resume; never returned by any API), `metadata` (the public projection shown with the job; scheduled runs write it, phase 4, with their identity; jobs of a host write it since phase 5a without `identity`, which is the record's `target` and which a snapshot of the record restores into `metadata.identity`), `progress` (last envelope), `result` or `error {code, message}`, `executor {kind: host|unit, pid, pid_start, unit, invocation_id}`, `resumable`, `checkpoint` (opaque per route, section 2.11), `protocol`.

States: `running`, `completed`, `failed`, `canceled`, `interrupted`. There is no `queued`: admission either starts the job or refuses it.

Write policy, from the spike measurements (1.4 ms fsync, 35 µs without, on NVMe):
- a record is written when the job starts and on every state change;
- fsync on state changes of jobs that have run for more than one second, and on every checkpoint; no fsync otherwise;
- progress goes to `/run`, never to `/var/lib`;
- a job that completes in under one second is deleted from the store when it completes, unless it failed. Such jobs never appear in the list or history. This is TrueNAS's `transient` without a flag.

Retention: terminal records are kept 30 days and at most 200 per uid; pruning runs at host start and daily, removing artifacts with records. The job store, `backend/common/jobs`, implements this policy; a host prunes at start and every 24 hours while it runs.

### 2.11 Job lifecycle

**Claim.** The host validates the request with the route decoder, builds the target, and checks the `id`: unknown → new job; known with the same route and request → attach; known with a different route or request → 409. Then admission: a running job of the same route with the same target → 409 `"<route> is already running on this target"`. Admission is per host, which is per uid; for admins the root host is shared, so privileged targets (pools, disks, containers, Docker, packages) are checked system-wide.

**Run.** The handler runs in a goroutine with a context cancelled only by `tasks.cancel`, stream abort, or host stop. `report` writes a progress frame to every attached stream and to the `/run` file (coalesced). The handler's log lines carry `JOB_ID=<id>`, `JOB_ROUTE=<route>` as journald fields through the existing slog handler.

**Finish.** The result frame goes to attached streams; the record is written `completed` or `failed`; a failed job raises an alert (section 2.13). A `completed` job younger than one second is dropped from the store.

**Cancel.** `tasks.cancel` or an abort sets `cancel_requested` in the record *first*, then cancels the context. The handler's own cleanup runs (removing a partial copy, `BALANCE_CTL cancel`, `SCRUB_CANCEL`, `DEV_REPLACE cancel`). The record becomes `canceled` only when the handler has returned. Until then the task snapshot reads `running` with `cancel_requested`, which a `task.snapshot` event on `tasks.events` announces.

**Interrupt.** A SIGTERM to the host with no `cancel_requested` on a job is a shutdown. For a non-resumable job the context is cancelled and the record is written `interrupted/shutdown`. For a resumable job the host calls the route's `Suspend(ctx, record)` hook with a deadline inside `TimeoutStopSec`; the hook returns the `checkpoint` and the record is written `interrupted/shutdown`, before the context is cancelled. The contract is generic: any route may declare the two hooks, and the host and the store know nothing about what they do. The first four resumable routes are the btrfs operations, because the kernel ties them to a process.

On a shutdown the host handles a route with a `Suspend` hook in this order: it flags the task interrupted (`Task.BeginInterrupt`), runs `Suspend`, writes the record `interrupted/shutdown` with the checkpoint, and only then cancels the job's context. The flag comes first so the handler's own cleanup, which the cancel starts, cannot turn the record `canceled`. A job whose record already has `cancel_requested`, because a `tasks.cancel` overlaps the SIGTERM, is left alone by the shutdown: it ends `canceled` within the drain.

| Route | Suspend | Resume |
|---|---|---|
| `storage.btrfs_balance` | `BALANCE_CTL pause`; checkpoint `{"paused":<bool>}`, true only when this call paused it. The kernel keeps the balance item and resumes it at the next rw mount | paused: resume it with the resume flag; running (the kernel resumed it at mount): observe with `BALANCE_PROGRESS`; gone with `paused=true`: finished (the kernel resumes a paused balance at mount and may end it before the host starts); gone with `paused=false`: run it again (`soft` when converting) |
| `storage.btrfs_scrub_start` | read each device's `last_physical`, `SCRUB_CANCEL`; checkpoint `{"remaining":{devid:offset}}`, or none (a full rescan) when nothing was read | restart each device's scrub from its stored offset; counters merge with the record of the canceled first part. An error in the extent straddling the cut is counted twice |
| `storage.btrfs_device_replace` | nothing: the interrupt's own context cancel cancels the kernel replace and the source is kept | follow a replace the kernel resumed at mount (`STARTED`); trust `FINISHED` only when the kernel's `time_started` is not before the job's original start, else fail with "start it again"; report `SUSPENDED` as "remount to continue". Device paths are never compared, because they are not stable across reboots |
| `storage.btrfs_device_remove` | nothing: the interrupt's context cancel sends `RM_DEV_V2 name=cancel`; moved chunks stay moved | issue the removal again; refuses a degraded pool or an unreadable device list |

Why the hooks differ, as the kernel behaves (7.0, section 5): a fatal signal, a SIGKILL included, cancels a balance in the kernel and deletes its item, so only a pause keeps it. A scrub has no kernel state, so the checkpoint is the offset. A replace is tracked by the kernel in an item and continued at mount by device id, so Resume follows it and never starts one on a path. Removal is never continued by the kernel, so Resume issues it again.

**Host start and boot.** Every host, before it serves, marks the records its uid's last host left `running`: `interrupted/reboot` when that host ran in an earlier boot (executors carry the boot ID; a power loss leaves records `running`, an orderly shutdown already marked them `shutdown`), `interrupted/crash` when it ran in this boot and is gone. A host touches only its own uid's store. `linuxio-jobs@0.service` is enabled, so the root host starts at boot, after `local-fs.target` and the degraded pool mounts (`linuxio-btrfs-degraded.service`). It runs the route's `Resume` hook, and writes the outcome to the same record, only for an `interrupted` record with reason `shutdown` or `reboot` that is `Resumable`, has no `cancel_requested`, and whose executor ran in an earlier boot (always uid 0). That is one resume, for the last shutdown. A record any other way interrupted (a crash, a host stopped in this boot by hand, a cancel that overlapped a stop) loses `Resumable` when the host starts, so a job is never resumed at some later reboot, weeks on, and there is no manual resume. A job the host cannot resume is alerted and stamped with its executor so later starts leave it alone. The jobs it marks and does not resume raise one alert each. The host then exits under its normal rule once the resumed jobs end.

The bridge binary has no verb besides `version`: nothing outside the browser starts a job, and the host serves no manual resume. Timers start no job either: scheduled work keeps its own unit and records itself (section 2.14).

#### Fault matrix

What each event does to a job, as phase 1 implements it:

- **Page reload, tab close, connection loss**: the stream detaches and the job runs on. Lost before the `task.snapshot` frame, the client resends the same envelope once (it attaches, or gets the stored outcome); lost after it, the client reattaches with `tasks.watch`. Each is tried once, so a resend that reached the job still reattaches once; a further loss is `outcome_unknown`.
- **Resend of an `id` with another route or request**: 409.
- **Logout, session expiry, bridge exit**: the bridge closes its host connection; jobs run on. The host exits one minute after its last bridge left and its last job ended. A later login of the uid sees the jobs the host still holds through `tasks.list` and `tasks.events`.
- **Host unreachable from the bridge**: mutations fail with 503 before anything starts; `tasks.get`, `tasks.cancel` and `tasks.watch` of a host job wait up to 10 s for the host, then answer 503; the bridge reconnects with backoff from 1 s to 30 s. An open `tasks.events` stream gets, on the next connection, the end of every job it saw running.
- **Host on another protocol (an update in between)**: new mutations the host would run fail with 409 `update_pending` (the three `BrowserBound()` ones run in the bridge and are not affected); what reads answer is in section 2.12; the old host finishes its jobs.
- **LinuxIO update**: hosts and their sockets are outside `linuxio.target`; the installer never stops them. A host still running the old binary answers a newer bridge `update_pending` until it exits; the next connection then starts the new one.
- **`systemctl stop` or orderly shutdown**: running jobs become `interrupted/shutdown`, resumable ones after `Suspend` stored a checkpoint and before their context is cancelled; the root host alerts. A job with `cancel_requested` is left to end `canceled`.
- **Host crash or kill**: the next start of that uid's host marks its `running` records whose pid and start time are gone `interrupted/crash`; the root host alerts.
- **Reboot or power loss**: the root host, started at boot, marks its `running` records `interrupted/reboot`, resumes the resumable ones (rule above) and alerts on the rest; a user's host marks theirs the same way when their next login connects.
- **Cancel** (`tasks.cancel`, stream abort): `cancel_requested` is written to the record, then the job's context is cancelled; the record becomes `canceled` only when the handler returns, whatever error it returns after the cancel, and the target stays taken until then. A snapshot stays `running` meanwhile and carries `cancel_requested`; the tray row and the storage controls read "Cancelling…" until the job ends (section 3.7).

### 2.12 Updates

- `linuxio.target` does not pull in hosts or their sockets. The installer never stops or restarts them; it may `daemon-reload`. On Ubuntu, needrestart, which apt runs after a dpkg run, restarts each service whose binary was replaced, and so restarted the job hosts once in the test VM, during an unattended-upgrades run (section 3.9, finding 2). Where `/etc/needrestart` exists the installer therefore installs, after the release binaries are verified and before it replaces any of them, and only when its content has the shipped form (`needrestart_override_valid`), an override (`packaging/etc/needrestart/conf.d/linuxio.conf` as `/etc/needrestart/conf.d/linuxio.conf`) that keeps needrestart from restarting the job hosts, the timer-run units and the updater's unit; the uninstaller removes it ([process-systemd-architecture.md](../process-systemd-architecture.md), needrestart). An in-app update runs the installer in a unit whose writable paths come from the bridge installed before the update; this build adds `/etc/needrestart/conf.d` to them as an optional entry, so the first in-app update launched by a build with it installs the file, while one launched by an earlier build (every release up to now) cannot write it and prints a notice, and the next update or a run of the installer from a terminal installs it (read from the source, not run).
- The hello frame carries a protocol number. Same number: carry on. Another number: the host refuses the hello (`426`), and to the bridge it is an older host that an update waits for. The older host exits under its normal rule, 60 s after its last job and its last bridge (a refused hello is no bridge); the bridge tries again with its backoff, at most 30 s later, and that connection starts the new binary.
- While an update waits, the bridge refuses every new mutation it would relay to the job host with `409` and a message that starts with the prefix `update_pending:` and then reads `LinuxIO update waits for N running jobs` (`1 running job`) while the older host runs jobs, `LinuxIO was updated; new jobs can start shortly` when it runs none, and `LinuxIO update waits for running jobs`, with no count, when the bridge cannot read the host's store. The shared mutation wrapper's default error toast shows the message without that prefix; a page that builds its own error text shows the message as received. The three `BrowserBound()` mutations, `filebrowser.upload`, `filebrowser.upload_batch` and `filebrowser.archive`, run in the bridge and are not affected. "Reads work" means, route by route (as built in phase 5a, section 3.9):
  - Answered by the bridge from the older host's records: `tasks.host`, with `update_pending: true`, the count and the running jobs (their label and last progress, without `metadata.identity` or `metadata.packageIds`). The bridge runs as the host's uid and reads that uid's job store read-only: the `running` records of executor kind `host` whose process still lives, and their progress files.
  - Answered empty at once: `tasks.history` (no entries) and `tasks.list` (the bridge's own tasks only); `tasks.events` sends none of the host's jobs.
  - Failing until the new host runs: `tasks.get`, `tasks.watch`, `tasks.cancel` and `tasks.output` of a host job wait up to 10 s for a host and answer `503`.
  - Calls and channels that do not go to the job host are not affected.
- The update banner (section 2.16) shows the count and names the jobs. The bell does not list them, and the banner cannot cancel them; the owner asked to revisit this (section 3.9, limits). Because `tasks.history` answers empty, the history page and `Past runs` say `History is unavailable until the LinuxIO update finishes.` in place of their empty texts, and refetch the history once the wait ends, without the user leaving the page (decided on 2026-11-10; section 2.16, section 3.9).
- The protocol number changes when a frame changes shape or a route moves between the bridge and the host; adding a route to the host does not change it. A host of the same protocol built before a route was added answers it `route not found`. Phase 5a adds no fallback for that: the last release (v0.52.0) has no job host, so only a development build of phase 4 is such a host.

### 2.13 Alerts

- Through `alerts.Raise` with source `jobs` and key `<id>`, titled by the job's name (its label; a file-browser job `<Action>: <label>`; else its route) and linked to `/history?job=<id>`: a `failed` job raises an alert (`error`) with the error message; an `interrupted` one a warning when a shutdown or a reboot stopped it, and an alert when its host crashed or it could not be resumed after the reboot. A `completed` job that ran five minutes or more raises a notice, `<name> finished` with how long it ran, when `browser_jobs` is `long_jobs` in the notification settings (plan 5b, section 3.10). A `canceled` job raises nothing.
- The alert store is root-owned, so only the root host raises alerts and reads the settings; a user host writes the record only. Right after a sign-in, an admin is told how many alerts are unseen and another user how many of their own jobs failed or were interrupted since they last saw the history (plan 5b, section 3.10).
- A scheduled script that fails or is interrupted raises the same jobs alert, from its unit's end hook or from the root host's liveness check (section 2.14); a completed one raises its schedule's one `finished` notice when the schedule's own choice, or else the scheduled scripts' line, is `every_run`. The timers' snapshots, replication, resyncs and Docker updates keep their own alerts, which follow the notification settings too ([Alerts](../alerts.md)).

### 2.14 Scheduled runs

Scheduled work stays in its own systemd unit (D8). It is never handed to a job host and no command starts a job. Each run writes its own record, shows in `tasks.list` and `tasks.events` while it runs, and keeps a copy of its output.

**Units covered.**

| Record `route` | Unit | Label | Owning page | Jobs alert |
|---|---|---|---|---|
| `scheduled.script` | `linuxio-schedule-<id>.service` | the schedule's name | `/services/schedules?schedule=<id>` | yes |
| `scheduled.btrfs_snapshot` | `linuxio-btrfs-snapshot.service` | Snapshots and replication | `/storage/subvolumes?view=snapshots` | no |
| `scheduled.btrfs_resync` | `linuxio-btrfs-resync@<uuid>.service` | Pool resync | `/storage?pool=<uuid>` | no |
| `scheduled.docker_update` | `linuxio-docker-update.service` | Container updates | `/docker/containers` | no |

Not covered: `linuxio-btrfs-health.service` (a check that only starts resyncs), the degraded mount at boot, the indexer's timer, the distribution's package auto-update.

**Two hooks per unit write the record.** They sit in packaged drop-ins, never in a rendered unit: `schedules.run_now` refuses a schedule whose unit file differs from what the build renders (`ensureInstalled`), so changing the rendered text would break every existing schedule.

```ini
[Service]
ExecStartPre=-+/usr/local/bin/linuxio scheduled-run begin --unit %n
ExecStopPost=-+/usr/local/bin/linuxio scheduled-run end --unit %n
```

`+` runs the hook as root whatever `User=` and the sandbox say; `-` keeps a failed hook from failing or blocking the run. The drop-in applies to every schedule through the prefix directory `linuxio-schedule-.service.d/`.

- `begin` creates the `running` record, fsynced so a power loss right after it cannot take it: `id` is `$INVOCATION_ID` written as a UUID, `route` and label come from the unit name, owner is root, `target` is `[unit]`, `metadata` is `{"label", "identity": [<schedule id or pool uuid>]}`, executor is `{kind: unit, unit, invocation_id, boot_id}`. `begin` runs as `ExecStartPre=`, so its time counts against the unit's start timeout, and a schedule's timeout can be as short as 1 s.
- `end` reads `$SERVICE_RESULT`, `$EXIT_CODE` and `$EXIT_STATUS`: `success` is `completed`; otherwise a record with `cancel_requested` is `canceled`, a system that is shutting down gives `interrupted/shutdown`, and anything else is `failed` with a message such as "exited with status 3", "killed by signal KILL" or "timed out". Then, in this order, it prunes the unit's older runs, saves the output (with a limit of its own, 5 s) and raises the alert, so a slow journal cannot starve the prune and a slow alert delivery cannot keep the output from being saved. The command gives itself 10 s in all; the alert is raised even when those have run out, and its delivery by webhook or mail (up to 15 s) can outlast them. A run whose `begin` wrote nothing still gets its record from `end`.

**Store.** `/var/lib/linuxio/jobs/scheduled` with progress under `/run/linuxio/jobs/scheduled`, root only. It is separate from the per-uid stores so scheduled runs have their own limit: each unit keeps its newest 50 ended runs and nothing older than 30 days. Every run is kept, also one shorter than a second.

**Output.** `end` copies the run's journal lines (`_SYSTEMD_INVOCATION_ID`) to `<id>.log` next to the record, after `journalctl --sync`. Each line carries its time: the journal's `short-iso` format, the same lines the live view shows. At most 10 MiB: a longer output keeps its first and last 5 MiB with one line between saying how many bytes were left out. The file is removed with the record. Stdout and stderr are one stream and cannot be told apart (checked on systemd 259: same `_STREAM_ID`, same priority; sending stderr through a second journal connection lost lines).

**Progress.** The btrfs timer program and the Docker update runner write the store's progress file of their own run, found through `$INVOCATION_ID`. Scripts report none; their feedback is their output.

**Job list, root host only.** `tasks.list` adds the running scheduled runs, `tasks.events` polls the store every 2 s while a client listens and sends snapshot, progress and terminal events, `tasks.get` answers for a scheduled run, `tasks.cancel` and `tasks.watch` of a running one answer 409. A `running` record counts only while systemd agrees: same boot, the unit's current invocation is the record's, and the unit is `activating`, `active`, `reloading` or `deactivating`. Otherwise the host marks it `interrupted` (`reboot` for an earlier boot, `crash` in this one). Every admin sees scheduled runs; other users do not.

**Stopping.** The bell shows no stop for a scheduled run; its row opens the owning page. `schedules.stop` and a scrub cancel that hits a timer's resync set `cancel_requested` on the record first, so the run ends `canceled`. `schedules.stop` names the invocation it stops and marks only that run's record, so a stop that arrives after its run ended cannot mark the next one; a scrub cancel knows no invocation and marks the resync unit's running record. Either takes the mark back when the stop or the cancel fails.

**Alerts.** One alert per failure. A script that fails raises the jobs alert (source `jobs`, key the record ID, title `<label> failed`, level `error`), as a failed browser job does; one that is interrupted raises `<label> was interrupted` as a warning when the system stopped, and as an alert when the root host found its unit gone in the same boot without the end hook having reported. With `every_run` (the schedule's own choice in `notifications.json`, or else the scheduled scripts' line), a completed run raises the notice `jobs/finished-<schedule id>`, `<label> finished`: one record per schedule, each run a new occurrence (plan 5b). Snapshots, replication, resync and Docker updates already raise their own alert and get no second one.

**Frontend.** The bell lists each running scheduled run with its label and progress. The Schedules page shows the output of the selected schedule's current or latest run, live, from the journal. Its past runs and their saved output, and the history page, came with phase 5a (sections 2.16 and 3.9).

### 2.15 Security

- Nothing new is reachable from the network. The webserver still relays bytes to a bridge that is the user.
- Authorization is the uid of the connecting process, read by the kernel. The root host accepts only root; a user host accepts only its uid. `linuxio-auth` starts a host only for the uid PAM admitted.
- The record contains the request, without the fields tagged `secret:"true"`. It is `0600`, owned by the uid, and no API returns it; snapshots expose `route`, `target`, `owner`, `progress`, `result`, `error` and timestamps, as today.
- The root host runs admin mutations as root, as the root bridge does now. The set of code running as root grows by one bounded `systemctl start` in `linuxio-auth`.
- A user host runs with `NoNewPrivileges`. The root host too; root does not need setuid binaries. If a handler turns out to need one, the unit drops the flag for uid 0 with a comment saying which.
- Ceilings: 256 running jobs per host; one host per uid; starting a running unit is a no-op, so repeated logins cannot multiply hosts.
- The 13 root-only routes the map found without `Privileged()` get it during migration.

### 2.16 Frontend

- `useMutation` everywhere; the three task hooks and `useCallMutation` are deleted after migration, with a codemod for the ~120 call sites.
- The running tray (`frontend/src/tasks/`) keeps its engines for uploads and downloads (bridge-local) and reads everything else from `tasks.events`. It shows running jobs older than one second, and every failed or interrupted one until dismissed.
- **History** (built in phase 5a, section 3.9). The bell's tray ends with a `History` link to `/history`, a page open to every signed-in user and not in the sidebar. The page takes its height from the main layout's content area, so its table has height to fill (the test VM found the table at zero height, finding 1 of section 3.9; fixed since and confirmed there). It reads `tasks.history` and lists ended jobs and scheduled runs, newest first: the job's title, its outcome (`Completed`, `Failed`, `Cancelled`, `Interrupted (<reason>)`), who started it, when it ended and how long it ran. The title is the job's label; for a file job it names what was done in the bell's words (`Copying 12 items`, `Moving report.pdf to Trash`, `Compressing backup.zip`, `Extracting backup`; `frontend/src/tasks/taskTitles.ts`), for a scheduled run it is its label alone, and a job without a label is named from its route. `Load older` asks for the next page with the last entry's end time as `before`, exactly as received. Selecting a row (`?job=<id>`) highlights it and shows its error, an `Open` link for a scheduled run, and, for a privileged session, the saved output. Admins see the root host's jobs and every scheduled run; other users see their own host's jobs.
- **Saved output.** A viewer reads the last 256 KiB through `tasks.output` and opens scrolled to its end. `Show earlier` reads the part before, which joins above the text while the text that was in view stays where it was; each part ends where the later one starts. When its query fetches again (after a failed `Show earlier` and a reconnect, or a remount) it rebuilds every part it had loaded. The text is shown as plain text.
- **Past runs.** Each schedule on the Schedules page lists its runs (`tasks.history` with its unit), newest first, under `Past runs`; selecting one shows its error and its saved output. A failed fetch shows its error in the card and raises no toast. While an update waits and the list is empty, the card says `History is unavailable until the LinuxIO update finishes.` in place of `This task has not run yet.`; the history page says the same in place of `Nothing has ended yet.` (decided on 2026-11-10, section 3.9). When the wait ends (the banner's flag goes from true to false), `JobsUpdateBanner`, mounted once for every signed-in user, invalidates every `tasks.history` query once, so both views load the real history without a reload. The live card `Output of the latest run` is unchanged.
- **Update banner.** For every signed-in user the layout asks `tasks.host` when the connection opens, again every 15 s while an update waits, and at once after a mutation started through `useMutation` fails with `update_pending` (the tray's `endpoint.start` callers do not trigger that); the wrapper's default error toast for that mutation shows the refusal without its `update_pending:` prefix. The banner is a `role="status"` region. It reads `LinuxIO was updated. New jobs wait for N running jobs to finish.` (`1 running job`), and `Show running jobs` / `Hide running jobs` lists each job in the banner, in a list of bounded height that scrolls: its title (as on the history page), who started it, when, and its last progress percentage, rounded (`· 43%`), when it reports one. With none running it reads `LinuxIO was updated. New jobs can start shortly.` and has no button. There is no button to force the update.
- **Not built (plan 5b).** At sign-in, the notice of the jobs that ended while the user was away; the notification settings (section 4.5).

### 2.17 Route classification

- `Call`: the 115 reads plus the five cache refreshes the map marks as not real changes (`docker.check_updates`, `docker.check_container_update`, `docker.get_icon`, `docker.get_icon_uri`, `monitoring.check_database`): 120, all `RetrySafe`.
- `Mutation`: the other 199 routes of the map. `tasks.cancel` stays a built-in. The 12 duplex channels are unchanged. Total 332.
- `BrowserBound`: `filebrowser.upload`, `filebrowser.upload_batch`, `filebrowser.archive`.
- `Resumable`: `storage.btrfs_scrub_start`, `storage.btrfs_balance`, `storage.btrfs_device_replace`, `storage.btrfs_device_remove`. Replication is not resumable: an interrupted run is reported and the hourly timer catches up.
- `Target()`: the request types behind the routes that declare `WithTaskMetadata` today implement it with the same identity. Request types of quick mutations do not implement it in this design; adding one is a per-type decision later, because a refused double click needs UI handling of its own.

### 2.18 Testing

- **Store**: claim, attach, 409 paths, write policy, pruning, crash detection by pid and start time, transient deletion. Unit tests.
- **Relay and protocol**: a bridge and a host in one test process over a socketpair: start, progress, result, detach, abort, resend attaches, protocol mismatch returns `update_pending`. Unit tests.
- **Host lifecycle**: exit rule with fake clock; SIGTERM marks running jobs and calls `Suspend` within the deadline. Unit tests.
- **Auth**: the hermetic PAM suite asserts the exact `systemctl` arguments of an unprivileged login and that a failed start keeps the login.
- **Host start**: at host start, only `shutdown` and `reboot` records of an earlier boot without `cancel_requested` are resumed (`TestResumeOnlyWhatTheLastShutdownInterrupted`), every other interrupted record loses `Resumable`, a `running` record with a dead pid of this boot becomes `interrupted/crash`, and the rest are alerted. Unit tests with the fake kernel for btrfs.
- **btrfs suspend and resume**: root tests in `TestBtrfsResumeIntegration` (`make test-btrfs-integration`; in the test VM `make vm-test-btrfs-integration`, which is the run that counts as kernel-verified): `signals`, `removeCancel`, `removeKill`, `rollback`, `balancePauseRemount`, `balancePauseSameMount`, `refusedWhilePaused`, `balanceKill`, `balanceCancel`, `scrubResume`, `scrubKill`, `replaceKill` and `replacePowerCut`. They kill the host mid-operation, pause and remount a balance, and resume a scrub from its stored offset; `replacePowerCut` builds the on-disk state of a power cut mid-replace from a frozen copy of the images. A run on the user's own hardware (`make test-btrfs-integration`, sudo) is optional and theirs to start.
- **Test VM browser specs**: `frontend/src/test/vm/jobs-phase3.spec.ts` starts each real route in the browser and cuts the host (section 3.6); `jobs-phase2.spec.ts` covers the generic host events (section 7).
- **Browser**: start a copy, log out, log in, find it running in the tray and completed in history (`make test-frontend-browser-quiet`).
- **Performance**: a test that starts a no-op mutation through the relay 1 000 times and asserts a median under 1 ms, so a regression in the relay is caught.
- **Guards**: the inventory test, the `Call` ⇒ `RetrySafe` test, the import boundary test, `logging/style_test.go` for the new log lines.

### 2.19 Measured, not estimated

On the development machine (NVMe, btrfs root), 2026-10-26: relay hop 27 µs median; record write 35 µs without fsync, 1.4 ms with; `StartTransientUnit` to a running process 5 ms on the user manager and 6.3 ms on the system manager; bridge binary start 8 ms warm; `docker stop` of a container that ignores SIGTERM 10.17 s. NAS hardware with a spinning root disk will show a slower fsync, which the write policy already accounts for.

## 3. Done

### 3.1 Phase 1: commit `c37d36c0`

`feat(jobs): run mutations in a per-user job host that outlives the login`.
Thirteen tasks, each implemented by a subagent, reviewed on Fable, and fixed
until the review was clean; then a whole-branch review and one fix wave.

| Task | What it built |
|---|---|
| 1 | Job store `backend/common/jobs`: records, write policy, transient deletion, pruning, crash detection by pid and start time |
| 2 | `peercred.Read` with the pid, `systemd.Notify` |
| 3 | `apischema.Mutation`, `MutationVoid`, `Progress[Detail]()`, `BrowserBound()`, `Target()` on request types, `ReportProgress` through the context |
| 4 | One stream per mutation: snapshot, progress, result; close detaches, abort cancels |
| 5 | The job host: hello, `SO_PEERCRED`, recorder, admission, idle exit, SIGTERM handling, the `interrupted` task state |
| 6 | Root launcher socket (removed by `0b44d7d3`) |
| 7 | Job client, the submit CLI and the host's resume call (CLI and call removed in Task 12) |
| 8 | Units, installer and the boot reconcile service (reconcile removed by `0c82d2d7`) |
| 9 | Bridge relay: one yamux session per bridge, `tasks.*` merged, reconnect with backoff, `update_pending` |
| 10 | `docker.update_container` as the first mutation; transient units and the durable worker deleted |
| 11 | Network bridge handoff on the job store; `durabletask` deleted |
| 12 | Frontend `useMutation`, envelope `id`, no client timeout, resend once |
| 13 | Docs, browser spec `jobs.spec.ts` (gated by `LINUXIO_E2E_DOCKER=1`), `make test-quiet` |

Final review fixes: a second update of a container waits for the Docker update
lock instead of failing after 10 s; a host stop interrupts live tasks so an
attached client reads `job was interrupted`, with a 5 s drain for the streams;
records with unknown members are kept, not pruned, so an older binary never
deletes what a newer one wrote; ten doc and comment fixes.

Fixes found by the first test on the installed build, folded into the same
commit: the client dials with retries, so a socket a crashed host left behind
no longer fails a submit; a standalone Docker update journal whose original
container is gone is cleared instead of refusing every later update; the
"waiting" phase shows only when the update lock is held.

### 3.2 Commit `0b44d7d3`: no world-connectable launcher

`fix(jobs): start a user's job host from linuxio-auth and drop the
world-connectable launcher`. The launcher was a 0666 root socket that started
a host for any local uid without PAM. `linuxio-auth` now starts the user's
host at login, after PAM and before the PAM session opens (so `pam_selinux`
cannot hand systemctl the user's exec context). The launcher package, verb and
units were removed; the installer retires them. The host unit gained
`TasksMax`, `MemoryMax=2G` and `OOMPolicy=continue`.

### 3.3 Commit `0c82d2d7`: socket activation, no host verbs, reconcile in the host

`feat(jobs): socket-activate job hosts and fold the boot reconcile into the
host`. A host that exited during a login stayed down until the next login,
because a user's bridge cannot start a system unit. Each host now has a socket
unit held by systemd, and a connection starts the host whenever it is down.
The `jobs host` and `jobs reconcile` verbs are gone: the host runs with no
arguments and recognises the socket name. The boot reconcile service and its
root writes into users' stores are gone: every host reconciles its own store
at start, executors carry the boot ID, and the root host is enabled at boot.
A host that cannot start closes the connections waiting for it; a resume that
fails is alerted and not retried in the same boot.

### 3.4 Verified on the development machine (2026-10-27)

All with the installed build of `0c82d2d7`, by the submit-CLI procedure
section 7 had then (in git history; section 7 now has the VM checks):

| Case | Result |
|---|---|
| Update through the host, detached client, stop mid-job, kill mid-job, failing job, same target twice, two targets at once | pass |
| Killed or stopped host comes back on the next connection | pass; a logged-in bridge brought it back within 1 s |
| Idle exit after 60 s, socket keeps listening, one connection wakes the host | pass |
| Non-admin login: socket `teste 600`, a stopped host wakes on that user's connection, root route refused with 403 | pass |
| Only `version` and the submit verb accepted; launcher socket and reconcile unit gone; root host enabled | pass |
| Orderly reboot with a job running: record `interrupted/shutdown`; root host starts at boot with nobody logged in | pass |
| Power-loss reboot: record `interrupted/reboot` marked by the root host at boot | not run (unit tests only) |

### 3.5 Phase 2 checked in the test VM (2026-11-05)

`make test-vm-browser` ran `frontend/src/test/vm/jobs-phase2.spec.ts`
against this tree (`e5f2e14d` with the VM rig), right after `make vm-reset`
and `make vm-install`, in Ubuntu 26.04 with kernel 7.0.0-38. It signs in as
the VM's sudo user, so every job ran in the root job host, and it started each
job in the browser. All 12 specs passed in 6.6 minutes, `login.spec.ts`
included. Section 7 describes the host events.

| Check | Result |
|---|---|
| Reload mid package update: a local package whose postinst sleeps 20 s | pass: the tray shows the job again; one `packages.update` record, `completed`; 2.0 installed |
| Reload mid Compose up: a service healthy after about 30 s | pass: the tray shows the job again; one `docker.compose` record, `completed`; both services up |
| Reload mid file copy: 2 GiB, the host's reads capped at 100 MB/s | pass: the tray shows `Copying` again; one `filebrowser.copy_batch` record, `completed`; the copy matches |
| Close the Compose dialog mid-run | pass: the dialog says the job runs on and the tray shows it; `completed`; both services up |
| Upload, batch upload, folder download as a zip | pass: the uploaded files and the zip's entries match the originals by SHA-256; no record failed (jobs under a second keep none) |
| Save a Compose stack over an existing file | pass: "File Already Exists" names the file; Overwrite writes the new content |
| Scrub across logout and login: pool on `vdb` and `vdc`, 1 GiB, 8 MiB/s per disk | pass: running after the new login, then `completed` and "Last check: …, 0 errors" |
| `systemctl stop linuxio-jobs@0.service` | pass: `interrupted/shutdown`, and the alert in the tray |
| `kill -9` of the host | pass: `interrupted/crash` once the bridge's reconnect started a new host |
| `virsh reboot` | pass: the root host started at boot with no login; record `interrupted/shutdown` |
| `virsh destroy`, then `virsh start` | pass: `interrupted/reboot`, and `job marked interrupted` in that boot's journal |

Seen on the way. None of these is a phase 2 failure:

- After a reload mid package update, the Updates page shows its loader until
  PackageKit ends the transaction, because its update list waits for it. Only
  the tray shows the running job.
- The scrub banner prints the percentage unrounded, for example
  `Checking for errors · 18.986434994665448%`.
- The journal field `LINUXIO_REASON` holds the reason JSON-quoted (`"reboot"`).
- A browser closed without signing out keeps its session, and with it one of
  the 16 connections `linuxio-auth.socket` accepts (`MaxConnections=16`). With
  16 such sessions, the next login fails with "could not start the session
  bridge". Fixed: the connection is the bridge's transport for the session's
  life by design, so the cap counts live sessions; it is now
  `MaxConnections=64`, and `make test-installation-scripts` keeps it there.
- A Compose stack saved by a sudo user gets a stack folder owned by root with
  mode 0700, inside that user's home.
- The VM's `clean` snapshot has `docker.io` without the Compose plugin and no
  `~/docker`. The spec installs `docker-compose-v2` and creates the folder.

### 3.6 Phase 3: storage safety and btrfs resume (commits `938e32c5`, `f8db05af`)

What the code does, as built. Where it differs from the plan text, this is
what holds; section 4.3 lists what moved out.

- **Cancel.** `cancel_requested` is recorded before the context is cancelled.
  The record turns `canceled` only when the handler returns, and any handler
  error after a cancel ends `canceled`. A cancel-requested job that overlaps a
  host SIGTERM is left alone by the shutdown and ends `canceled` within the
  drain. A job is never resumed after a cancel.
- **Shutdown and resume** (D4). A route with `Suspend` is flagged interrupted
  first (`Task.BeginInterrupt`), then suspended, then recorded
  `interrupted/shutdown` with its checkpoint, then its context is cancelled.
  At host start only `interrupted` records with reason `shutdown` or `reboot`,
  `Resumable`, no `cancel_requested` and an executor from an earlier boot are
  resumed; every other interrupted record loses `Resumable`. No manual resume.
- **The four hooks** are in the table of section 2.11.
- **Signals.** Long ioctls (scrub, replace, balance, removal) run on a locked
  OS thread with async signals blocked, because kernels from 6.19 abort a
  scrub or replace on any pending signal (section 5). A freeze or
  suspend-to-RAM still aborts them. Positive kernel refusal codes, such as 8
  for "exclusive operation in progress", are errors now; a refused remove or
  balance used to read as success.
- **Balance.** `BALANCE_CTL` was passed by pointer, so pause and cancel failed
  with `EINVAL` on real kernels before phase 3: the cancel button never
  worked. Fixed.
- **Scrub.** `SCRUB_CANCEL` is refused while a replace is `STARTED`, because
  the kernel would cancel the replace with it.
- **Device remove.** Cancel sends `RM_DEV_V2 name=cancel` (kernel 5.14 or
  later), resent while the kernel answers `ENOTCONN`. A cancel that lands as a
  removal finishes can cancel the next relocation at its start.
- **Pool create.** A failed or canceled create rolls back: only superblocks
  with the create's own fsid are wiped, before only the partitions it created
  are deleted, and never once the pool is in fstab. A disk overwritten with
  `force` is not restored ([btrfs.md](../btrfs.md#pools)).
- **Docker storage switch** (D1). The files are written while Docker runs and
  one systemd `RestartUnit` follows; the job waits for a new `InvocationID`.
- **Replace across a power cut** (11c). udev holds back a pool whose replace
  target (devid 0) makes the device count one too high, so systemd never
  mounted it and the kernel could not continue the replace.
  `linuxio-btrfs-degraded` now mounts such a pool at boot with its fstab
  options, without `degraded`, after the same 30 s grace; the kernel continues
  the replace and the job follows it ([btrfs.md](../btrfs.md#missing-disks)).

**Checked in the test VM** (kernel 7.0.0-38, Ubuntu 26.04, the sudo user so
every job ran in the root host): `make test-vm-browser` runs the phase 2 and
phase 3 specs, `login.spec.ts` included, 19 checks, all passing after the
replace fix; `make vm-test-btrfs-integration` passes `TestBtrfsResumeIntegration`
(including `replacePowerCut`), `TestBtrfsDegradedIntegration`,
`TestBtrfsIntegration` and `TestPartitionIntegration`. `make test-quiet`
passed on the phase's code.

**Checked on the development machine** (kernel 7.0.0-43, 2026-11-09, at
`07d04f7b`, with sudo): `make test-btrfs-integration` passes the same four
tests, every `TestBtrfsResumeIntegration` step included. `replaceKill` took
the 6.19-and-later branch (the kill cancels the replace and `ResumeReplace`
refuses it), so the branch for earlier kernels is still not exercised.
`balanceKill` passed in this run.

| Check | Result |
|---|---|
| Balance across `systemctl stop` and a reboot (data raid1 to raid0, 256 MiB chunks, disks capped at 32 MiB/s) | pass: `interrupted/shutdown`, checkpoint `{"paused":true}`, sysfs `balance paused`; after the boot the kernel logs `balance: resume`, the host `job resumed`, `completed` |
| Scrub across a power cut | pass: `interrupted/reboot`, `job resumed`, `completed`; a full rescan, since nothing was checkpointed |
| Replace across a power cut | pass after 11c: the pool is mounted at boot, the kernel continues the replace and the job follows it. Before 11c it failed as described above |
| Scrub cancel, then reboot | pass: the record stays `canceled`, no `job resumed`, no kernel scrub |
| Crash (`kill -9`), then reboot | pass: stays `interrupted/crash`, `Resumable` cleared, no `job resumed`; the alert shows in the tray |
| Pool create rollback (a file where the mountpoint goes) | pass: `failed`, `rolled back failed btrfs pool create`, no partition, no btrfs magic on the disk |
| Docker switch kill | pass: `interrupted/crash`, Docker running on the new data root, the alert in the tray |

Seen on the way. None of these blocks phase 3:

- No browser cancel path set `cancel_requested` for a scrub: the banner's
  "Stop check" called `storage.btrfs_scrub_cancel` and bypassed `tasks.cancel`,
  and the tray did not list scrubs or balances. The cancel-then-SIGTERM path
  was covered by unit tests only (since checked in the VM, section 3.7). If
  such a stop raced a host SIGTERM, the
  record would read `interrupted/shutdown` without `cancel_requested` and be
  resumed (inferred from the code, window of a few milliseconds). Resolved for
  scrubs: "Stop check" now cancels the scrub's job through `tasks.cancel` and
  calls `storage.btrfs_scrub_cancel` only for a scrub without a job (the
  timer's), and the tray lists scrub jobs with a cancel. Balances follow the
  same pattern: the Devices card's "Cancel" cancels the running balance job
  through `tasks.cancel` and calls `storage.btrfs_balance_cancel` only for a
  balance without a running job, and the tray lists balance jobs with a cancel
  (the outcome toast stays with the Devices card).
- The "was interrupted" alert stayed after a successful resume (balance,
  scrub). Fixed: the root host resolves it when the resumed job completes
  (`TestResumedJobThatCompletesClearsItsInterruptedAlert`).
- The overview and the bridge's mount count still treat a replace target as a
  present disk, so a pool that is both missing a member and mid-replace shows
  **Mount** where the kernel needs **Mount degraded**. The boot path counts
  correctly. Fixed in `4d73aac5`: the offline pool carries a missing count
  that discounts the replace target, and the handler and the overview read it.
- The Docker kill always landed after the restart was queued. The accepted gap
  (files written, restart not yet requested) was not exercised.
- A pause waits for the chunk in hand: with the default 1 GiB data chunks on
  slow disks, `systemctl stop` can take a while.
- The root test step `balanceKill` is flaky on a nearly full shared test pool
  (the kernel aborts a transaction with ENOSPC); it passed in every other run.
  Explained and fixed in section 3.7.

### 3.7 Phase 3 follow-ups (2026-11-09)

Six items the phase left open, as built.

- **A cancel shows in the UI.** A canceled job keeps running until its handler
  returns (a balance or a removal finishes the chunk in hand). The task
  snapshot now carries `cancel_requested`, set when a running task is
  canceled; `tasks.cancel` returns it and `tasks.events` sends a
  `task.snapshot` event for it, which also drops a progress event the
  throttle held back, since that one is older. The event is never sent after
  the task's terminal event, which would bring an ended job back into a
  client's list (`TestCancelRequestIsNotAnnouncedAfterTheTaskEnded`). The tray
  keeps the row of a job it cancels, reading "Cancelling…" with the cancel
  disabled, and reports
  "Task cancelled" when the job ends; before, it removed the row at once and
  the next event of the still running job brought it back. A cancel from a
  page or another session marks the row the same way. The Devices card's
  **Cancel** and the banner's **Stop check** read "Cancelling…" and
  "Stopping…" and are disabled until the job ends. Device removal and replace
  still have no cancel control and no tray row.
- **Device removal reports progress**: the share of the device's allocation
  the kernel has freed (`DEV_INFO` `bytes_used`, which the kernel lowers as
  each chunk goes). A resumed removal counts from 0 for what is left.
- **A replace that finished as the user canceled** (resume path only) ends
  `completed`: after the cancel the handler follows the replace again, for at
  most 30 seconds, and takes a `FINISHED` one as the success it is, growing
  a bigger disk as usual.
- **The source of a followed replace is checked.** `ResumeReplace` reads the
  replace item of the dev tree (`src_devid`) and refuses a running replace of
  another device, and `CancelReplace` leaves such a replace running. The
  kernel names no source once the replace ended, or when a degraded mount
  lacked it; those are followed, and canceled, as before.
- **Cancel, then a host stop** is checked in the VM: a balance canceled in
  the browser with the disks slowed to 2 MiB/s, `systemctl stop` while the
  record still reads `running` with `cancel_requested`, then a reboot. The
  record ends `canceled`, nothing is resumed and the kernel logs no
  `balance: resume`.
- **`balanceKill` flake.** By that step every interrupted balance had left
  half-filled chunks, and the 4 GiB test disks were 70 to 90 % allocated
  (2878 to 3693 MiB of 4096 across four runs), so a relocation could find no
  unallocated room. With 3.3 GiB disks the balance steps fail every time with
  the kernel's `enospc errors during balance`. The disks are 8 GiB now
  (sparse, 4384 MiB allocated at that step), and a failed run logs the
  kernel's btrfs messages.

Checked in the test VM (kernel 7.0.0-42): `make vm-test-btrfs-integration`
passes its four tests, with the removal reporting a rise on a real kernel and
`ResumeReplace` refusing device 1's job while device 2 is replaced;
`make test-vm-browser` passes its 21 checks, the new one included.
`make test-quiet` passes. On the development machine (kernel 7.0.0-43,
2026-11-10, at `f930876b`, with sudo) `make test-btrfs-integration` passes its
four tests too: the resumed removals report a rise, `ResumeReplace` refuses
device 1's job during the replace of device 2, and `balanceKill` runs with
4384 of 8192 MiB allocated.

Checked on a kernel before 6.19 (2026-11-10): a second test VM on Ubuntu
24.04, kernel 6.8.0-147 (`make vm-test-btrfs-integration VM_RELEASE=noble`,
[development.md](../development.md#test-vm)), passes the four tests twice.
There a signal does not abort the unmasked scrub of the `signals` step, and
the killed replace of `replaceKill` runs to the end (`FINISHED`), which
`ResumeReplace` accepts: the branch that had never run. The removal cancel,
the removal progress and the refusal of another device's replace job behave
as on 7.0.

### 3.8 Phase 4: scheduled runs, checked in the test VM (2026-11-10)

What the code does, as built, read from the source (the VM table further
down is what was observed). Section 2.14 is the design; where the two differ,
this holds.

- **Hooks.** `packaging/systemd/linuxio-run-record.conf` holds the two lines.
  The installer copies it as `10-linuxio-run.conf` into
  `linuxio-schedule-.service.d`, `linuxio-btrfs-snapshot.service.d` and
  `linuxio-docker-update.service.d` under `/etc/systemd/system`, then reloads
  systemd; `linuxio-btrfs-resync@.service` carries the lines itself. What
  `renderService`, `btrfs.RenderUnit` and `renderDockerUpdateUnit` write is
  unchanged, and a guard test in each of those packages fails on a hook line:
  the btrfs test compares the text byte for byte, the schedules and Docker
  update tests refuse the hook lines in it.
- **Command.** `linuxio scheduled-run begin|end --unit <unit>`
  (`backend/cli/scheduled_run.go`) calls `backend/common/scheduledrun` with a
  10 s deadline. `begin` writes its record with an fsync
  (`jobs.Store.CreateSynced`), so a power loss right after the start cannot
  take the record that the boot's liveness check marks `interrupted`. `end`
  writes the record first, then prunes, then copies the output under its own
  5 s limit, then raises the alert, even when the 10 s have run out; alert
  delivery (webhook and mail, up to 15 s) can outlast the command's deadline
  and the unit's stop timeout, by which time the record, the prune and the
  output are done. `begin` runs as `ExecStartPre=`, so its time counts against
  the unit's start timeout; a schedule's timeout can be as short as 1 s.
- **Store.** `jobs.NewScheduledStore()`: `/var/lib/linuxio/jobs/scheduled`
  and `/run/linuxio/jobs/scheduled`, both `0700 root`, files `0600` (seen in
  the VM). `PrunePerTarget` keeps the newest 50 ended runs of each unit, none
  older than 30 days; a run's output and progress files go with its record.
  The progress file of an ended run stays in `/run` until then.
- **Output.** `journalctl --sync`, then `--no-pager --quiet --all -o
  short-iso _SYSTEMD_INVOCATION_ID=<id>`, through a writer that keeps the
  first and last 5 MiB. Without `--all`, journalctl prints `[N blob data]`
  for a line holding a carriage return, a colour code or invalid UTF-8;
  without `--quiet` an empty run would save `-- No entries --`. systemd 259
  writes the time with a colon in the offset: `2026-11-10T09:21:47+00:00`.
- **Progress.** The btrfs timer program (snapshots, replication, resync) and
  the Docker update runner write their run's progress file
  (`scheduledrun.ReporterFromEnv`). The Docker message is `Updating <name>`,
  with no count. `ReporterFromEnv` logs at Debug a run record it finds but
  cannot read (a missing one is silent); the Docker update runner builds it
  after its logging is set up. Neither timer program turns debug logging on,
  so the line shows only where a build or a test does.
- **Root host.** `tasks.list`, `tasks.events` and `tasks.get` serve the
  scheduled runs; `tasks.cancel` and `tasks.watch` of a running one answer
  `409` ([API Contract](../api-contract.md#scheduled-runs)). The liveness
  check runs at host start and at every listing. A run no longer runs when
  its unit is not loaded (systemd's `NoSuchUnit`), runs a different
  invocation, or is in any state but `activating`, `active`, `reloading` or
  `deactivating`. Of the D-Bus errors only `NoSuchUnit` means that: any other
  leaves the record alone, so a bus hiccup cannot raise a false alert.
- **Stopping.** `schedules.stop` sets `cancel_requested` on the record of
  the invocation it names (`scheduledrun.RequestCancel` with that invocation
  ID), then stops that invocation, so a stop that arrives after its run ended
  marks nothing. `storage.btrfs_scrub_cancel` marks the running record of the
  pool's resync unit (it knows no invocation), then cancels the scrub, which
  ends the resync; it does not stop the unit. When the stop or the cancel
  fails, either takes the mark back (`scheduledrun.WithdrawCancel`); a failed
  mark or withdraw is logged at Warn.
- **Frontend.** The bell lists a scheduled run with its label and progress,
  capped at 99 % so a row never reads as complete, and an **Open** link
  (`Open <label>`) to the owning page where other rows have **Cancel task**.
  When a new events stream opens, the scheduled rows are dropped and come back
  from its opening snapshots. The Schedules page shows "Output of the latest
  run", `logs.service.follow` with `invocationId` and `--all`.
- **Alerts.** A script that fails or is interrupted raises `jobs/<run id>`
  ([Alerts](../alerts.md)); the other three kinds raise none of their own.

**Checked in the test VM** (Ubuntu 26.04, kernel 7.0.0-38, systemd 259.5, as
the sudo user, after `make vm-reset` and `make vm-install`):
`make test-vm-browser PLAYWRIGHT_ARGS='login.spec.ts jobs-phase4.spec.ts'`
runs `frontend/src/test/vm/jobs-phase4.spec.ts`. Each script is a schedule
created on the Schedules page, disabled, with a calendar of 2099, and started
with **Run now**; the snapshot and Docker update units are started with
`systemctl start`, as their timers would start them. On a fresh VM all 13
checks pass in 3.5 minutes, `login.spec.ts` included (final fix wave,
2026-11-10, with the end order, the named cancel mark and the fsynced begin
of this section). Earlier runs failed only on faults of the spec itself
(below); no check needed a change to the product. On a VM where a snapshot
rule or the Docker update setting was ever saved, the drop-in check skips the
part that needs a missing unit and says why.

| Check | Result |
|---|---|
| A drop-in alone creates no unit | pass: the three `10-linuxio-run.conf` are installed, and `linuxio-btrfs-snapshot.service` and `linuxio-docker-update.service` read `not-found` until a rule or a setting is saved |
| A running script (`echo first; sleep 60; echo last >&2`) | pass: the bell lists the schedule's name with no **Cancel task**; **Open** leads to `/services/schedules?schedule=<id>`; the output card shows `first` while it runs; one record, `scheduled.script`, `completed`; the saved output is exactly two lines, `<time> linuxio-e2e linuxio-schedule[<pid>]: first` and `…: last` |
| A failing script (`echo boom; exit 3`) | pass: `failed`, "exited with status 3"; the alert "`<name>` failed" with that message, in the bell after a reload (it reads alerts once a minute) |
| Stop on the Schedules page (`sleep 300`) | pass: `canceled` with `cancel_requested`, no alert |
| A long output (300 numbered lines, `line 001 ` to `line 300 ` and 45000 bytes each, 13.5 MB) | pass: 10485794 bytes saved, 10 MiB and the 34-byte line saying how many bytes were left out; the saved output begins with `line 001` and ends with `line 300`, both whole with their time, and the line saying what was left out lies between them |
| The last line before the exit (`echo almost; echo last`, 10 runs in a row) | pass: every saved output ends with `: last`, so `--sync` is enough and `end` needs no wait |
| Control characters (`printf 'a\rb\033[31mred\033[0m\n'`) | pass: the saved output holds `a\rb` and the escape sequences, and no `blob data` |
| A broken hook (`/usr/local/bin/linuxio` moved aside) | pass: the script ran; systemd logged both hooks as `Unable to locate executable '/usr/local/bin/linuxio'` and the unit `Deactivated successfully`; the run left no record |
| The snapshot unit (a rule saved on the Storage page, then `systemctl start`) | pass: one `scheduled.btrfs_snapshot` record, `completed`, label "Snapshots and replication" |
| The Docker update unit (scheduled checks enabled in Settings, then `systemctl start`, no containers) | pass: one `scheduled.docker_update` record, `completed`, label "Container updates" |
| A power cut (`sleep 300`, `virsh destroy`, `start`) | pass: `interrupted/reboot`, marked by the root host; the alert "`<name>` was interrupted" (polled for), and in the bell |
| An orderly reboot (`sleep 300`, `virsh reboot`) | pass: `interrupted/shutdown`, written by the end hook as the system stopped (systemd logs the unit `Failed with result 'signal'`); the same alert. The bus still answered for `SystemState` then |

**An alert webhook that never answers** (checked by hand in the same VM,
not by the spec): with `webhookUrl` set to `http://10.255.255.1:9/` in
`/var/lib/linuxio/notifications.json`, a failing script started with
`systemctl start` left its record `failed` at 10:47:11.596, its `.log` at
11.641 and its alert in `alerts.json` at 11.647; the hook's
`alert delivery failed … webhook: context deadline exceeded` came at
10:47:26.656, and only then systemd logged `State 'stop-post' timed out.
Terminating.` The stop timeout cut off nothing but the delivery. The setting
was removed again afterwards (there was none before).

**On systemd 255** (the second VM, Ubuntu 24.04.5, kernel 6.8.0-147,
systemd 255.4, after `make vm-install VM_RELEASE=noble`):
`make test-vm-browser VM_RELEASE=noble PLAYWRIGHT_ARGS='jobs-phase4.spec.ts -g "last line before the exit|a running script"'`
passes both: the running script's bell row, link, live output and two saved
lines, and ten runs in a row whose saved output each ends with `: last`. So
`journalctl --sync` makes the last lines readable on 255 too. systemd 255
also writes the offset as `+00:00`. The other checks were not run there.

Seen while numbering the long output's lines: journald starts a new entry
when another process writes to the same stream in the middle of a line.
`printf 'line 001 '` from the shell followed by `head | tr` saved
`line 001` (its trailing space gone) and the run of `x` as two lines with two
PIDs. The live view shows the same entries. The spec now has the shell write
each line whole.

`make vm-test-btrfs-integration` passes `TestBtrfsDegradedIntegration` (its
resync goes through the changed `Resync` call), `TestBtrfsResumeIntegration`,
`TestBtrfsIntegration` and `TestPartitionIntegration` on the same VM.

**An update, simulated.** The VM did not install the previous release. With
this build installed, a schedule was created; the three drop-in directories
were removed and systemd reloaded, to stand for an install from before
phase 4. Run now then ran the script (`upgraded` in its journal) with no
`ExecStartPre=` and left no record. `make vm-install` put the drop-ins back
without touching the schedule's unit or definition (their times stayed those
of the creation), and Run now, with nothing saved again, left one record,
`completed`. A real install of an older release over which this one is
installed was not tried.

`make test-quiet` passes on the tree of the final fix wave: both linters, the
types, the installer fixtures, the C and PAM tests, the frontend's 1965 tests
and every Go package, `bridge/handlers/filebrowser` included (from the test
cache on that tree; run afresh in 1.3 s on the tree just before the wave's
last test and document edits). Two earlier runs that day had hung in that package's
`TestOwnedUploadReplacesASymlinkSwappedInAfterPrepare` until the 10-minute
timeout (and one also failed `monitoring/internal/app`
`TestStartContextDisabledHTTP`); both packages are outside phase 4 and
unchanged by it. The hang's stacks hold no phase 4 code: the test goroutine
waits writing to a `net.Pipe` that nothing reads any more, from the file
browser's own test helpers.

**Inferred, not observed:**

- No run was started by its timer: Run now and `systemctl start` start the
  same service, with the same drop-in.
- `scheduled.btrfs_resync` never ran in the VM: no check returned a missing
  disk. Its unit file carries the two lines like the drop-in;
  `TestBtrfsDegradedIntegration` runs the resync through the changed
  `Resync` call, and unit tests cover the reporter. `RunReplication`'s new
  progress path is covered by unit tests only.
- The bell's progress for a snapshot or a Docker update was not seen: those
  runs end within a second or two. The snapshot run's progress file was
  (`{"message":"Taking snapshots"}`). A Docker update with containers to
  update, and its `Updating <name>` message, did not run.
- The `409` answers of `tasks.cancel` and `tasks.watch`, the 50-run and
  30-day prune, a non-admin seeing no scheduled run, a scrub cancel that
  marks a timer's resync, a stale stop that marks nothing and a failed stop or
  cancel that takes its mark back are covered by unit tests only. The fsync
  of `begin`'s record is read from the source: no test observes it, and the
  power-cut test runs `sync` first.
- The live output card was checked for plain text only, not for a line
  holding control characters.

Seen on the way:

- The plan's example of a saved line had the offset as `+0000`; systemd 259
  writes `+00:00`, on the VM and on the development machine alike. The plan is
  corrected and the spec accepts both.
- The spec's own two faults: it looked for the snapshot rules on
  `/storage?pool=<uuid>`, where they are not (they are on
  `/storage/subvolumes?view=snapshots`), and a rule's switch shows the saved
  rule, so it turns on only after the rules reload. Playwright's `check()`
  gives up before that; the spec clicks and then waits.
- systemd unloads a script's unit once it stops when nothing holds it, as
  here, where the schedules' timers were never started. `systemctl show`
  then loads it again: `InvocationID` reads empty (seen) and `Result` its
  default, `success`, whatever the run did. The spec reads a run's outcome
  from its record or the unit's journal, never from those properties.

### 3.9 Phase 5a: history, past runs and the update banner (2026-11-10)

[Plan](../superpowers/plans/2026-11-10-jobs-phase5a-history-and-banner.md),
eight tasks, not yet committed. What the code does, as built, read from the
source; the VM table further down is what was observed. Where the plan text
and this differ, this holds.

- **Records.** A job started in the browser keeps its public metadata (label,
  path, action and the like) in its record's `metadata`, as scheduled runs
  do, so an ended job keeps its title. The request is never copied into it
  (`jobshost/recorder.go`). Its identity is stored once, as the record's
  `target`, and not repeated in `metadata`; every snapshot built from the
  record (`tasks.get`, `tasks.watch`, `tasks.list`, `tasks.events`, a resend's
  answer) restores `metadata.identity` from the target, so those answers are
  unchanged. Scheduled-run records keep their identity in `metadata`, and older
  records that still carry one there are read as they are. A record too large
  to store (1 MiB) sheds, in this order, its result, its target, its request
  and, last, its metadata (`backend/common/jobs/store.go`), so a batch of
  thousands of paths keeps its record.
- **`tasks.history`, `tasks.output`, `tasks.host`.** Three Calls of the
  reserved `tasks.*` namespace, declared in
  `backend/bridge/handlers/tasks/handlers.go` and served by `TaskService`, the
  bridge's relay and the job host. Their requests, answers, callers and errors
  are in the [API Contract](../api-contract.md#ended-jobs-saved-output-and-updates).
  Only `tasks.output` is `Privileged()`. A history page never splits entries
  that ended at the same instant, so it can be longer than its limit; the
  cursor `before` is the last entry's end time exactly as received
  (`finished_at`, else `started_at`, else `created_at`). History entries are
  slim, so a page of large records stays well inside a relay frame
  (privilege review F1, below): no entry carries the job's result or its last
  progress; a host job's entry carries no `metadata.identity` and no
  `metadata.packageIds`; `error.message` is cut to at most 4096 bytes on a
  character boundary, with `…` appended; a scheduled run keeps its identity,
  which its `Open` link needs. `tasks.get` still answers the full snapshot.
  The jobs `tasks.host` lists while an update waits leave out
  `metadata.identity` and `metadata.packageIds` too, and keep their label and
  last progress. An output chunk is at most 1 MiB, 256 KiB by default, starts
  on a character boundary and reports its real `offset`.
- **While an update waits** (section 2.12). The bridge answers `tasks.host`
  from the older host's records, answers `tasks.history` and `tasks.list`
  empty, and lets `tasks.get`, `tasks.watch`, `tasks.cancel` and
  `tasks.output` wait 10 s and fail with `503`. A refused mutation (always
  `409`) names the count, says `LinuxIO was updated; new jobs can start
  shortly` when nothing runs, and names no count when the store cannot be
  read; the shared mutation wrapper's default error toast shows it without its
  `update_pending:` prefix. The
  three `BrowserBound()` mutations run in the bridge and are not refused. Otherwise `tasks.history` waits up to 10 s until the bridge has a
  connection or its last attempt was refused for an older build (a failed
  attempt does not end the wait), and answers `503` when neither comes.
- **Frontend** (section 2.16). The `History` link at the foot of the bell's
  tray and the `/history` page; the saved output viewer
  (`frontend/src/components/tasks/SavedOutput.tsx`), on that page for a
  privileged session and under `Past runs` on the Schedules page; the update
  banner (`JobsUpdateBanner`) beside the application's own update banner.
- **No fallback** for a job host of the same protocol built before these
  routes: it answers them `route not found`. The last release (v0.52.0) has no
  job host, so only a development build of phase 4 can be such a host; the
  history page then shows its error and the banner nothing until that host
  exits.

**Make targets run**, one Make at a time (Tasks 1 to 7 from their reports):

| Task | Targets | Result |
|---|---|---|
| 1, record metadata | `make test-go` (jobshost; the new test failing first), `make golint-quiet` | pass |
| 2, `tasks.history` | `make test-go` (six packages), `make generate`, `make golint-quiet`, `make test-quiet` | pass |
| 3, `tasks.output` | `make test-go` (six packages; two deliberate breaks of the code shown failing), `make generate`, `make golint-quiet` (one shadowed `err` fixed); `make test-quiet` run on its tree during the review | pass |
| 4, the relay while an update waits | `make test-go` (five packages, race detector), `make generate`, `make golint-quiet` (one staticcheck finding fixed), `make test-quiet` | pass |
| 5, history page and viewer | `make test-frontend` per file, `make check-frontend-quiet` (309 files, 1990 tests) | pass, after one round of review fixes |
| 6, past runs | `make test-frontend` per file, `make check-frontend-quiet` | pass, after one round of review fixes |
| 7, update banner | `make test-frontend` per file, `make check-frontend-quiet` | pass |
| 8, the VM spec and these documents | `make lint-quiet`, `make tsc-quiet`; `make test-quiet` (frontend 311 files and 2022 tests run; every Go package passed from the test cache, the Go code unchanged since its last run); `make build-nocheck` of the update build (below); `make vm-reset`, `make vm-install` and `make test-vm-browser-quiet`, twice; `make check-docs-quiet` | the gates pass; the VM runs below |
| 8, fix round 1 (spec and documents only) | `make lint-quiet`, `make tsc-quiet`, `make check-docs-quiet`; the VM was not run in this round | pass |
| 8, final round (after the final code fixes) | `make test-quiet` (311 frontend files and 2034 tests; Go packages from the test cache, unchanged since the fix round's own run); `make vm-reset`, `make vm-install`, `make build-nocheck` of the update build, `make test-vm-browser-quiet` (run 3), `make vm-reset`; `make check-docs-quiet`, `make lint-quiet` | pass; run 3: 7 of 7 |
| Follow-up B, the needrestart override (finding 2) | `shellcheck` and `shfmt -d` on the three changed scripts; `make test-installation-scripts-quiet`, `make lint-quiet`, `make test-quiet` (312 frontend files and 2047 tests; Go packages from the test cache, no Go change); `make vm-reset`, `make vm-install`, `make build-nocheck` of the update build, `make test-vm-browser-quiet` (run 4), `make vm-reset`; `make check-docs-quiet` | pass; run 4: 8 of 8 |
| Follow-up B, the updater's writable paths (an optional `/etc/needrestart/conf.d`) | `make test-go GO_TEST_PKGS=./bridge/handlers/appupdate/...`, `make golint-quiet`; `shellcheck` and `shfmt -d` on the installer and its fixtures, `make test-installation-scripts-quiet`, `make test-quiet`, `make check-docs-quiet`; the VM was not run again: the installer's changed notice runs only when the write fails, and the path list only in an in-app update, neither of which run 4's checks reach | pass |
| Follow-up B, review fixes (the snippet's content check, its step after the checksums, a private staging directory, notices and documents) | `shellcheck` and `shfmt -d` on the installer and its fixtures, `make test-installation-scripts-quiet`, `make lint-quiet`, `make check-docs-quiet`; neither `make test-quiet` nor the VM in this round | pass |
| Follow-up B, VM confirmation of the review fixes (run 5) | `make test-quiet` (312 frontend files and 2047 tests), `make vm-reset`, `make vm-install`, `make test-vm-browser-quiet` (run 5), the refusal check over `make vm-ssh`, `make vm-reset`; `make check-docs-quiet` | pass; run 5: 2 of 2 |

**Checked in the test VM** (Ubuntu 26.04.1 LTS, kernel 7.0.0-38-generic,
systemd 259.5, as `lsb_release -ds`, `uname -r` and `systemctl --version`
printed over `make vm-ssh` after run 2 and again after run 3; each run after
`make vm-reset` and `make vm-install`):
`make test-vm-browser-quiet PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5a.spec.ts'`
runs `frontend/src/test/vm/jobs-phase5a.spec.ts`, with `LINUXIO_VM_UPDATE_BUILD`
naming the update build. The checks sign in as the sudo user, except the one
that creates a user without sudo over SSH. Run 1 (14:07): 2 of 7 passed. Run 2
(14:19), after the spec changes listed below: 3 of 7 passed, `login.spec.ts`
included. Every failure of run 2 is a product finding below, not a fault of
the spec. Run 3 (15:40), after the final code fixes and the spec's last
changes: **7 of 7 passed**, in 3.8 minutes (its table follows run 2's). The
results come from the runs' Playwright output; what the tables add from the
VM's journal and stores was read over `make vm-ssh` after the run, and is
marked so.

| Check | Result (run 2) |
|---|---|
| A finished job in the history: a scrub of the test pool at 64 MiB/s per disk, then the bell's `History` | **fail** (finding 1). Following `History` closes the tray and opens `/history`; the page shows its title and no table rows. The spec saw the job's record reach `completed`; its label `Scrubbing btrfs filesystem` and owner `linuxio-e2e` were read from the store over `make vm-ssh` after the run |
| A failed scheduled script, `echo boom; exit 3` | **fail** (finding 1): no row. Before that: the record is `failed`, and `tasks.output` from the admin's session answers `offset: 0`, the whole `size` and a `content` equal byte for byte to the saved output (`…: boom`) |
| A schedule's past runs: a script run twice that prints its `$INVOCATION_ID` | pass (both runs). `Past runs` lists two runs `… · Completed · …`, newest first; selecting the older shows its saved output (its invocation ID) and not the newer's |
| A long output: 600 numbered lines of 1000 characters, 637 800 bytes saved | **fail** on one soft check (finding 3). Opened by its link `/history?job=<id>`. The viewer holds `line 600` and not `line 001`; `Show earlier` is offered; two presses load the rest and the button goes; the text then holds lines 1 to 600 once each, in order and whole, so no part is lost or doubled where chunks meet; lines 1 and 2 are then in view. At opening the box shows lines 355 and 356, the start of the last 256 KiB, not the last line (the spec's log: `opened with lines 355 to 356 in view`). Fixed since; see run 3 |
| A user without sudo: `e2e-plain`, created over SSH; a 1 GiB copy in the file browser, the user's host reading at most 100 MB/s | **fail** (finding 1): no row. Before that: the copy ran in that user's job host and left a `completed` record in `/var/lib/linuxio/jobs/<uid>/`; `tasks.output` of an admin's scheduled run, from that user's session, answers `403` (the bridge logs `forbidden: privileged route tasks.output requires elevated bridge`) |
| The update banner: a scrub at 1 MiB/s per disk, then the update build installed over the running root host | pass. The install left the root host running (same `MainPID`) and the scrub running. After a new sign-in the banner reads `LinuxIO was updated. New jobs wait for 1 running job to finish.`, and the host logs `job host refused a bridge hello: protocol 3, want 2`. `Show running jobs` lists one job, `Scrubbing btrfs filesystem · linuxio-e2e · started …`, and then reads `Hide running jobs`. Saving a new schedule is refused with the toast `update_pending: LinuxIO update waits for 1 running job`, and no record appears. `tasks.history` answers no entries at once; `tasks.get` of the scrub answers `503` after 10 s. With its cap lifted the scrub completed; the banner then read `LinuxIO was updated. New jobs can start shortly.` and was gone within the spec's bound of 150 s, and the root host's `MainPID` was new. Read over `make vm-ssh` after the run (the scrub's record and `journalctl -u linuxio-jobs@0.service`): the scrub finished at 14:22:27.457, the older host logged `job host stopped` at 14:23:27.286 (59.8 s later), and systemd logged `Starting linuxio-jobs@0.service` at 14:23:43.161 (15.9 s after that), 30 s after the hello it refused at 14:23:13.153. In run 1 this check failed on finding 2 |
| `login.spec.ts` | pass |

The spec changed between the runs without touching what an assertion
requires: the two checks that call `tasks.output` do it before the page's rows,
so the route's answer is seen despite finding 1; the long output is opened by
its link, as the row click cannot work; the update check stops the apt timers
and waits for an apt run in progress (finding 2), checks the older host's
`MainPID` once more before the scrub's end, and reads the empty history from
the route, because the page's "Nothing has ended yet." sits in the zero-height
table (Playwright called it visible in run 1, but the screenshot shows nothing).

After run 2, with findings 1 and 3 fixed in the product, the spec changed
again: the long output's check requires the decided behaviour of the viewer
(it opens on line 600; across each `Show earlier` the line at the top of the
view stays in view; scrolled to its top, the box shows line 1); the update
check requires the host's `protocol N, want M` line to be there before it
compares the numbers, and waits for apt until neither unit is active,
activating, deactivating or reloading. For the final code fixes it finds the
banner by `role="status"`, expects the refusal's toast without its
`update_pending:` prefix, checks a second refusal once nothing runs, expects
the copy's history row to read `Copying big.bin`, and checks, softly, that the
banner's job line ends with a percentage.

| Check | Result (run 3, the last run before the follow-ups) |
|---|---|
| A finished job in the history | pass. Following `History` closes the tray and opens `/history`; the table is visible, and its first row names `Scrubbing btrfs filesystem`, `Completed` and `linuxio-e2e`; clicking it selects `?job=<the scrub's id>` |
| A failed scheduled script | pass. `tasks.output` from the admin's session answers the saved output byte for byte; the row reads `Failed`; selecting it shows the alert `exited with status 3`, a viewer with `…: boom`, and an `Open` link to `/services/schedules?schedule=<id>` |
| A schedule's past runs | pass, as in runs 1 and 2 |
| A long output (638 400 bytes saved) | pass. The viewer opens with lines 599 and 600 in view; at each of the two `Show earlier` presses, line 599, at the top of the view before the press, is still in view after it (the spec's log: `press 1: line 599 was at the top; lines 599 to 600 in view`, and the same for press 2); lines 1 to 600 appear once each, in order and whole; scrolled to its top, the box shows line 1 first |
| A user without sudo | pass. `tasks.output` of the admin's run answers `403`; `/history` has one row, which reads `Copying big.bin`, `Completed` and `e2e-plain`; the private schedule appears nowhere |
| The update banner | pass, with every check of run 2, and also: the banner is found as a `role="status"` region; its job line reads `Scrubbing btrfs filesystem · linuxio-e2e · started 11/10/2026, 3:42:06 PM · 1%` (the spec's log); the refusal's toast reads `LinuxIO update waits for 1 running job`, without the prefix; once the scrub had ended, a new job was refused with the toast `LinuxIO was updated; new jobs can start shortly` while the older host still ran. Read over `make vm-ssh` after the run: the scrub finished at 15:42:31.927 and the bridge logged the second refusal at 15:42:34.497; the older host logged `job host stopped` at 15:43:32.894 (61.0 s after the scrub); systemd logged `Starting linuxio-jobs@0.service` at 15:43:47.410, 14.5 s later and 30.0 s after the hello it refused at 15:43:17.404. Both scrubs' records hold a `metadata` with the label and no identity |
| `login.spec.ts` | pass |

The VM was reset to its clean snapshot (`make vm-reset`) after run 3.

**Run 4** (22:13, after the needrestart override of finding 2 and the history
sentence of the limits below; the same VM, kernel and systemd as run 3,
needrestart 3.11-1ubuntu2 as `dpkg-query -W needrestart` printed): `make
vm-reset`, `make vm-install`, `make build-nocheck` of the update build, then
`make test-vm-browser-quiet PLAYWRIGHT_ARGS='login.spec.ts
jobs-phase5a.spec.ts jobs-needrestart.spec.ts'` with `LINUXIO_VM_UPDATE_BUILD`
naming the update build: **8 of 8 passed**, in 3.8 minutes. `make vm-install`
logged `Installed /etc/needrestart/conf.d/linuxio.conf`, as did both installs
in the specs. What the table adds from the VM's journal and store was read over
`make vm-ssh` after the run.

| Check | Result (run 4) |
|---|---|
| needrestart and a replaced job host (`jobs-needrestart.spec.ts`): a scrub at 1 MiB/s per disk, then the installed build installed again with the rig's `install`, and `linuxio-monitoring`'s binary replaced by a copy renamed over it | pass. The install left the root host running (same `MainPID`; its `/proc/<pid>/exe` read `/usr/local/bin/linuxio-bridge (deleted)`) and the scrub `running`. Control, with the override moved out of `conf.d`: `override_rc` held 6 entries fewer, and `needrestart -r l -l` listed `systemctl restart linuxio-jobs@0.service` and `systemctl restart linuxio-monitoring.service` under `Services to be restarted:`. With the override: list mode named only `linuxio-monitoring.service` there and `linuxio-jobs@0.service` under `Service restarts being deferred:`; `needrestart -r a -l` printed the same under `Restarting services...` and the deferred heading, exited 0, gave `linuxio-monitoring.service` a new `MainPID` and left the host's; the scrub was still `running` and then, with its cap lifted, `completed` in the same host. Read over `make vm-ssh` after the run: that host started at 22:13:48.573 and stopped at 22:16:59.844, after the update build's install of the last check, with no restart between; systemd restarted `linuxio-monitoring.service` at 22:14:02.649 and 22:14:04.778 (the install) and at 22:14:11.247 (the needrestart run, by its timing); the scrub finished at 22:14:13.872 |
| The five checks of run 3 before the update banner, and `login.spec.ts` | pass |
| The update banner, with the history page open in a second page of the session | pass, with every check of run 3, and also: during the wait `/history` showed its table with `History is unavailable until the LinuxIO update finishes.` and without `Nothing has ended yet.`; after the banner was gone and the root host's `MainPID` was new, the same page, not reloaded, showed the scrub's row within the spec's bound of 30 s, and the sentence was gone. The banner's job line read `Scrubbing btrfs filesystem · linuxio-e2e · started 11/10/2026, 10:15:34 PM · 1%`. Read over `make vm-ssh` after the run, from `journalctl -b -u linuxio-jobs@0.service -o short-precise`: the older host logged `job host refused a bridge hello: protocol 3, want 2` seven times, the first at 22:15:44.232, the scrub finished at 22:15:59.544, the older host logged `job host stopped` at 22:16:59.844 (60.3 s later), and systemd logged `Starting linuxio-jobs@0.service` at 22:17:15.270 (15.4 s after that). No apt unit ran in this boot |

The VM was reset to its clean snapshot after run 4.

**Run 5** (22:46, after the review fixes to the installer: the snippet's
content check, its step after the checksums and the `mktemp -d` staging
directory): `make test-quiet`, `make vm-reset`, `make vm-install`, then `make
test-vm-browser-quiet PLAYWRIGHT_ARGS='login.spec.ts jobs-needrestart.spec.ts'`
(no update build; the phase 5a spec was not run again): **2 of 2 passed**, in
35.8 s, `jobs-needrestart.spec.ts` (31.5 s) and `login.spec.ts` (3.7 s).
`make vm-install` ran the changed installer end to end through its local path;
it printed `Installing needrestart override...` and `Installed
/etc/needrestart/conf.d/linuxio.conf` after `Steps 1-2 — Verify Local Build`
and before `Step 3/5 — Install Release Files`, and ended `Installation
complete!`. Read over `make vm-ssh` after it: the file is mode 644, owned by
`root:root`, with the checkout's SHA-256; no `/tmp/linuxio-install*` directory
was left; `needrestart -v -r l -l` evaluated the file and exited 0. The needrestart
check printed the same three needrestart reports as in run 4. Then, by hand
over `make vm-ssh`, the refusal path: a copy of the rig's checkout in the VM
(`Makefile`, `LICENSE`, `docs/THIRD_PARTY_NOTICES.md` and `packaging/`), whose
snippet had `system("id");` appended, installed with its own
`install-linuxio-binaries.sh --local --build-dir ~/linuxio/build` as root: the
installer printed `The needrestart override was not installed: the fetched
file is not what LinuxIO ships`, went on, exited 0 and ended `Installation
complete!`; the installed file kept its inode, modification time and SHA-256
and holds no `system("id")`; no staging directory was left; `linuxio.target`,
`linuxio-webserver.socket` and `linuxio-monitoring.service` were active, and
needrestart's list mode still exited 0. The VM was reset to its clean snapshot
after run 5.

Found in the VM, in the product:

1. **The history page showed no rows** (fixed after run 2; confirmed in
   run 3). `HistoryPage` laid its table out as the Schedules page
   does: a flex column whose table wrapper is `flex: 1 1 0; min-height: 0`,
   around an `AppVirtualTable` with `fillAvailable`. The Schedules page sits
   in `RoutedTabLayout`, which bounds its height; `/history` had no such
   parent, so the wrapper was 0 px high, the table's `height: 100%` was 0, and
   the virtualiser rendered no row. The accessibility tree held the header row
   and an empty body, Playwright called the table hidden, and the screenshot
   showed the title and description only. Seen in runs 1 and 2; it failed three
   checks. The page's unit tests run in jsdom, which has no layout. The page's
   root now takes its height from the main layout's content area, as the logs
   page does. Run 3: confirmed fixed in the real layout; the
   table is visible, its rows are found in three checks and a row is
   selected in two.
2. **needrestart restarts the job hosts after an update** (fixed after run 3;
   checked in run 4, above). In run 1,
   `apt-daily-upgrade.service` ran unattended-upgrades from 14:12:51 to
   14:14:42, the fresh VM's first daily run. After the dpkg run that ended at
   14:13:57 its needrestart hook (`/etc/apt/apt.conf.d/99needrestart`)
   logged `systemctl restart linuxio-jobs@0.service linuxio-jobs@1001.service`
   in `/var/log/unattended-upgrades/unattended-upgrades-dpkg.log`, because both
   hosts ran the `linuxio-bridge` the install had replaced seconds before;
   systemd ran the two restart jobs at 14:13:55. The root host interrupted the
   running scrub (`interrupted/shutdown`) and a host of the new build started
   at once. needrestart is installed by default on Ubuntu and its hook runs
   after every dpkg run, so an apt run after a LinuxIO update can interrupt
   the jobs the update was waiting for (observed once, in this
   unattended-upgrades run): the
   installer leaves the hosts alone, the distribution's tooling does not. A
   host restarted in the same boot clears `Resumable` (section 2.11), so such a
   scrub is not resumed; that is read from the source, not observed here.
   The owner decided on 2026-11-10 that the installer ships a needrestart
   override. `packaging/etc/needrestart/conf.d/linuxio.conf` adds to
   needrestart's `$nrconf{override_rc}` one regular expression with the value
   0 ("do not restart") for each of `linuxio-jobs@<uid>.service`,
   `linuxio-schedule-*.service`, `linuxio-btrfs-snapshot.service`,
   `linuxio-btrfs-resync@*.service`, `linuxio-docker-update.service` and
   `linuxio-updater-*.service`; the daemons the installer restarts itself are
   not listed. The installer installs it as
   `/etc/needrestart/conf.d/linuxio.conf` where `/etc/needrestart` exists,
   after the release binaries are verified and before it replaces any of
   them, and only when every line is blank, a comment or one such entry for a
   `linuxio-` unit (`needrestart_override_valid`: the file is Perl that
   needrestart runs as root, and a release install fetches it from `main`
   without a checksum); the uninstaller removes it
   ([process-systemd-architecture.md](../process-systemd-architecture.md),
   needrestart). The in-app updater's writable paths gain an optional
   `/etc/needrestart/conf.d`, which reaches only updates launched by a build
   that has it (limits, below). The syntax was read in the VM, from
   needrestart 3.11-1ubuntu2 (`dpkg-query -W needrestart`): `needrestart.conf`
   sets `override_rc` as a hash of regular expressions ("the first matching
   regex will be used") and then evaluates each `conf.d/*.conf` as Perl;
   `/usr/sbin/needrestart`, in list and automatic mode, puts a service whose
   matching value is 0 under "Service restarts being deferred" and restarts
   the others; `man needrestart` gives `-r l` (list only), `-r a` (restart
   automatically) and `-l` (libraries only). The VM saw
   `linuxio-jobs@0.service` deferred and `linuxio-monitoring.service`
   restarted (`jobs-needrestart.spec.ts`, run 4); the installer fixture tests
   cover the install cases, the content check, the uninstall and, with Perl,
   all six expressions.
3. **The saved output opened at the start of its last part** (fixed after
   run 2; confirmed in run 3). The viewer loaded the last
   256 KiB but did not scroll, so it opened on the first lines of that part
   (lines 355 and 356 of 600 here) rather than on the end. As decided since,
   it opens scrolled to its end, and `Show earlier` keeps the text that was in
   view where it was while the earlier text joins above it. Run 3:
   confirmed fixed; it opened with lines 599 and 600 in view, and the
   line at the top stayed in view across both presses.

**Privilege review** of the change as it stood after Task 7 (a reviewer the
run's controller dispatched before VM run 1): no Critical or Important finding. F1:
`tasks.history` returned each record's result and last progress, which the
page never uses and which could make a page larger than a relay frame; fixed
in the final fix round, with the slim entries described above. F2, a known
limit with no fix: on the root host every admin's history shows other admins'
failed jobs, with their error messages and metadata; admins are
root-equivalent, and `tasks.get` already returns any record by its ID.

**Final review** of the whole change (after fix round 1): no Critical
finding; four Important. Three were fixed in the final fix round: a host
job's record repeated its identity in `metadata` (it could push a large
batch's target, or its whole record, over the size limit; now stored once, as
the target), the saved output viewer lost its end when its query fetched
again, and history rows did not name what a file job did. The fourth is left
for the owner: while an update waits, the history page and `Past runs` read
as empty; decided on 2026-11-10 and built (limits, below). The same round also changed the refusal text with
nothing running, dropped the prefix from the refusal's toast, made the banner
a status region with a bounded list and each job's percentage, highlighted
the selected history row and silenced the `Past runs` query's toast.

Unit tests only:

- `Load older` (it needs more than 100 ended entries), pages through entries
  that ended at the same instant, the nanoseconds of the cursor, the `400`
  answers of both routes, a chunk asked to start inside a character, a run
  without saved output and a link in place of `<id>.log`.
- The slim history entries and `tasks.host` jobs (no result, progress,
  identity or package IDs; error messages cut at 4096 bytes), the shedding
  order of an oversized record, a batch of thousands of paths, a refetch of
  the saved output after a failed `Show earlier`, and the refusal text when
  the store cannot be read. Run 3 saw only that the scrubs' records keep no
  identity in `metadata` and that the history and the banner still show them.

Inferred or not checked:

- A real update from an older release. The older host here is this tree with
  the protocol one lower than the installed build; its records have the same
  format, so reading the records of an older build was not exercised.
- More than one waiting job, a user's host as the older host, and a waiting
  job being cancelled (its `cancel_requested` is not carried): not checked.
- `tasks.watch`, `tasks.cancel` and `tasks.output` while an update waits: only
  `tasks.get` was seen failing with `503`; that the others do too is read from
  the source, where they share its wait.
- The `BrowserBound()` uploads and archive running while an update waits: read
  from the source, not checked.
- The banner after a failed poll, and how a screen reader announces it: not
  checked.

Known limits, by decision or deferred by the reviews:

- While an update waits the bell does not list the jobs being waited for and
  they cannot be cancelled from the banner, which names them itself (the bell
  is fed by `tasks.events` and `tasks.watch`, which an older host cannot
  serve). The owner asked to revisit this before phase 5a is called done:
  keep it, have the relay serve `tasks.events` and `tasks.watch` from the
  records, or find whether a cancel can reach a job on an older host at all.
- While an update waits, `tasks.history` answers an empty history, so the
  history page and `Past runs` would read as empty. Decided on 2026-11-10 and
  built: with an empty list they say `History is unavailable until the LinuxIO
  update finishes.` in place of `Nothing has ended yet.` and `This task has not
  run yet.`; entries already loaded stay, a failed query still shows its
  error, and when the wait ends every `tasks.history` query is refetched once
  so the pages fill without a reload (`useRefreshHistoryAfterUpdate`, called
  by `JobsUpdateBanner`). The bridge reading the history from the store itself
  in that state stays a possible later change. VM confirmation (scenario 6 of
  `jobs-phase5a.spec.ts`, run 4): while the update waited, `/history` showed
  `History is unavailable until the LinuxIO update finishes.` and not `Nothing
  has ended yet.`; once the wait had ended, the same page, not reloaded, showed
  the scrub's row and no longer the sentence. `Past runs` during the wait was
  not checked in the VM.
- No fallback for an older host of the same protocol (above).
- A refused mutation names no count when the bridge cannot read the store
  (`update_pending: LinuxIO update waits for running jobs`). With none
  running the banner reads `New jobs can start shortly.` with no button, and a
  refused mutation `LinuxIO was updated; new jobs can start shortly`. After
  the older host exits, `tasks.host` answers `update_pending: true, running: 0`
  until the bridge's next attempt, at most 30 s later (15.9 s in run 2, from
  the root host's journal).
- A history page can be longer than its limit when many entries ended at the
  same instant (bounded by the stores' retention: 200 per uid, 50 per unit).
- Up to three bytes of invalid data at the start of a chunk are not shown.
- With a job host that cannot start, the history page shows an error after
  10 s rather than an empty list.
- The running snapshots of `tasks.host` carry no `cancel_requested`.
- Each `tasks.host` poll while an update waits reads the store again, so a
  corrupt record logs a warning every 15 s.
- A row named by `?job=` that is not in the loaded pages shows nothing and no
  message; plan 5b links alerts to it.
- History rows are selected with the mouse only, as on the Schedules page.
- A failed `Load older` shows the page's error above the loaded table and a
  toast as well.
- `Past runs` shows an error without saying what failed, and nothing while
  its first page loads.
- A failed banner poll hides the banner until the next good one, and a list
  once opened opens again when jobs return.
- A record whose error message alone is over the 1 MiB record limit still
  fails to be written when the job ends, as before phase 5a.
- The store reads check a file, then open it, in two steps
  (`openRegularFile`); the directories belong to root or to the uid itself.
- On the root host every admin's history shows the other admins' failed jobs,
  with their error messages and metadata (privilege review F2).
- The needrestart override of finding 2 and in-app updates. The bridge
  installed before an update starts the updater unit (`ProtectSystem=full`)
  with its own writable paths (`updaterWritablePaths` in
  `backend/bridge/handlers/appupdate`). This build adds
  `/etc/needrestart/conf.d` to them as an optional entry, so the first in-app
  update launched by a build with it installs the override. An in-app update
  launched by an earlier build, every release up to now included, cannot write
  it: the installer prints a notice, the install goes on, and the next update
  or a run of the installer from a terminal installs it. Read from the source
  and from the installer fixtures, which simulate a read-only `conf.d` and a
  read-only needrestart directory; no in-app update was run, and the VM run 4
  above did not exercise this.

**The update build** for the banner check is this tree with `jobs.Protocol`
one higher, made without touching the tree through a Go overlay:

```sh
cp backend/common/jobs/record.go <dir>/record.go   # raise Protocol by one in the copy
printf '{"Replace":{"%s":"%s"}}\n' "$PWD/backend/common/jobs/record.go" "<dir>/record.go" > <dir>/overlay.json
make build-nocheck BIN_DIR=<dir>/build GO_BUILD_EXTRA_ENV="GOFLAGS='-buildvcs=false -overlay=<dir>/overlay.json'"
make vm-reset && make vm-install
LINUXIO_VM_UPDATE_BUILD=<dir>/build make test-vm-browser PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5a.spec.ts'
```

Without `LINUXIO_VM_UPDATE_BUILD` the check is skipped and says why. In this
run the two builds' `linuxio-indexer`, which does not use `jobs`, were the
same byte for byte, while `linuxio-bridge` and `linuxio-webserver` (which
embeds the bridge's hash) differed. The check leaves the update build
installed.

Seen on the way:

- A fresh VM runs its first unattended-upgrades a few minutes after boot. In
  run 1 it installed kernel 7.0.0-42 (not booted; the checks ran on 7.0.0-38)
  and restarted journald, udisks2, polkit and ssh as well as the job hosts.
- The bridge of the user without sudo logged `route failed: forbidden:
  privileged route monitoring.get_cpu_history requires elevated bridge` (and
  the network and disk histories, and `filebrowser.subfolders`) while that user
  was on the dashboard and in the file browser. Not part of phase 5a; not
  looked into.

### 3.10 Phase 5b: configurable notifications and the notice at sign-in (2026-11-11)

[Plan](../superpowers/plans/2026-11-10-jobs-phase5b-notifications.md), nine
tasks, not yet committed. What the code does is described in
[Alerts](../alerts.md), the canonical document, and in the
[API Contract](../api-contract.md#notification-settings); where the plan's
text and the decisions taken while it was built differ (section 4.5), the
decisions are what was built. Below: what each task built, read from the
source and the task reports; the Make targets run; what the test VM showed;
what only unit tests or the source cover; and what is left open.

**Built.**

- **Task 1, the settings.** `notifications.json` gains `report` (per kind),
  `scripts` (per schedule) and `levels` (level by channel), with
  `ReportFor`, `ReportForScript` and `ChannelsFor`; an older file loads into
  the behaviour it had, and every save still writes `minSeverity` and
  `dockerUpdates` for an older build. Every write is one locked read, change
  and atomic write under `notifications.json.lock`. A parse error never quotes
  the file. The wire types carry `levels` and `report`
  (`backend/common/alerts/settings.go`, `backend/bridge/apischema/alerts.go`,
  `backend/bridge/handlers/alerts/operations.go`).
- **Task 2, the grid.** Email and webhook follow each level's ticks
  (`backend/common/alerts/deliver.go`); `alerts.list` lists and counts only
  the levels ticked for the browser, errors always; mark all seen marks only
  what is listed; the test notification ignores the grid; a refused save
  answers 400; `alerts.get_notifications` over a file that does not parse
  answers the defaults with `settingsUnreadable`.
- **Task 3, jobs and scripts.** The root job host names a job in its alert
  (file jobs `<Action>: <label>`), links `/history?job=<id>`, raises an
  interruption by a shutdown or a reboot as a warning (a crash, and a job
  that could not be resumed, stay alerts after the final fix round) and,
  under `long_jobs`, a notice for a completed
  job of five minutes or more (`backend/bridge/internal/jobshost/`). A
  script's end hook raises one `jobs/finished-<schedule id>` notice per
  schedule under `every_run` (`backend/common/scheduledrun/`).
  `alerts.RanFor` writes durations; `alerts.NotificationsOrDefaults` reads
  the settings for producers; a full store evicts the lowest level first.
- **Task 4, snapshots and resync.** `RunReplication` also returns how many
  jobs sent a snapshot; the timer's pass raises the `btrfs/snapshot-run`
  notice under `snapshots: changes` or `every_run`
  (`backend/btrfs-timer/snapshot.go`); `resynced-<uuid>` follows `resync`
  (`backend/common/btrfs/degraded.go`).
- **Task 5, updates.** The Docker updater reads the settings once per pass,
  always raises failures, raises "updated" under `changes` and `every_run`,
  and the pass notice `linuxio-update-pass` under `every_run`
  (`backend/common/dockerupdate/`). System updates offer no `never` (APT three
  policies, DNF and Mint two), `AutoUpdateState.failures_unreported` reports
  a stored `never`, and a provider that cannot tell what it installed says
  "Automatic updates ran" (`backend/cli/alert.go`,
  `backend/bridge/handlers/packages/internal/autoupdate/`).
- **Task 6, a schedule's own choice.** `notify` on `ScheduleStatus`,
  `schedules.create` and `schedules.update`, kept in `notifications.json`; an
  update of only the choice writes nothing else; the dialog's *Notify me*
  select (`backend/bridge/handlers/schedules/`, `SchedulesTab.tsx`).
- **Task 7, the settings page.** The cards *What to hear about*, *Where each
  level goes* and *Delivery*, *Save* under them, the warning and *Report
  failures* for a stored `never`, and the warning for an unreadable file
  (`NotificationSettingsSection.tsx`).
- **Task 8, the notice at sign-in.** The toast for an admin and for another
  user (`useAwayNotice.ts`), `openNotificationsPanel()`, the bell's focus on
  its trigger, `dismissals.historySeenAt`, and the history page's sentence for
  a job outside the loaded entries.
- **Task 9.** `frontend/src/test/vm/jobs-phase5b.spec.ts`, the schedule
  helpers it shares in `frontend/src/test/vm/vm.ts`, and these documents.
- **Final fix round** (after the final review, Rulings 21 to 25 in 4.5): an
  interruption by a crash, a job that could not be resumed and a script whose
  end hook never reported are alerts again, a shutdown or a reboot a warning;
  the reads polled while `notifications.json` does not parse (`alerts.list`,
  `schedules.list`, `schedules.get`) log at debug level; webhook errors name
  only the URL's host; `historySeenAt` is stored in UTC and never ahead of the
  bridge's clock; a save that sends `levels` or `report` ignores the echoed
  `minSeverity` or `dockerUpdates`; a schedule's choice over an unparsable
  file, or over a stored file that validation refuses, answers 409; a page
  save over an unparsable file keeps a copy of it as
  `notifications.json.unreadable` (made only when the save goes through); deleting a schedule resolves its
  `finished` notice and refetches the bell; an updating Docker pass that
  replaced nothing reports like a Check only pass; saving the settings
  refetches the bell; the page's copy states the levels of interruptions and
  of storage health, and the "not reported" warning shows only while
  automatic updates are on; `SaveNotifications`, which had no caller, is
  removed; the Schedules page's details area is capped at half the page.

**Make targets run**, one Make at a time (Tasks 1 to 8 from their reports and
the run's ledger). Since 2026-11-11 a Go standard-library advisory
(GO-2026-6859, `net/http` of Go 1.27.3, fixed in 1.27.4) makes the
govulncheck step of `make golint-quiet`, and so of `make test-quiet`, fail on
the Go release `backend/go.mod` pins, for the whole repository and not
because of this work. From then on the gates ran with `GO_VERSION=1.27.4`;
`backend/go.mod` was not changed.

| Task | Targets | Result |
|---|---|---|
| 1 | `make test-go` (store and handlers; failing first; a deliberate break of the lock and one of the parse error shown failing), `make generate`, `make tsc-quiet`, `make test-frontend` (the page), `make lint-quiet`, `make golint-quiet`, `make check-frontend-quiet`, `make test-quiet` | pass. The first `make test-quiet` failed once in `TestOwnedUploadRefusesASymlinkAtTheTarget` (`bridge/handlers/filebrowser`, untouched), which passed alone three times and on the rerun |
| 2 | `make test-go` (failing first, twice), `make generate`, `make golint-quiet`, `make check-frontend-quiet`, `make test-quiet` | pass |
| 3 | `make test-go` (failing first), `make golint-quiet`, `make test-quiet`; fix round: `make test-go` (the three job-stream tests 30 times on one CPU), `make golint-quiet` | pass. One `make test-quiet` failed once in `TestHostRaisesAlertForFailedAndInterruptedJobs` (`Serve did not return` after the 5 s stream drain), passed on the rerun and 200 of 200 times alone; the fix round removed its cause (the tests now close each job stream) |
| 4 | `make test-go` (failing first), `make golint-quiet`, `make test-quiet`; round 2: `make test-go`, `make golint-quiet` | pass |
| 5 | `make test-go` (failing first), `make generate`, `make golint-quiet`, `make check-frontend-quiet`, `make test-quiet`; fix rounds: `make test-go`; then `make golint-quiet` and `make test-quiet` stopped at the advisory; the controller's `make golint-quiet GO_VERSION=1.27.4` | pass in the end. The first `make golint-quiet` failed on a complexity finding in its own test (fixed); in fix round 1 it stopped at type errors in Task 6's tests, then mid-change; in fix round 2 `make golint-quiet` and `make test-quiet` stopped at the advisory (every other lane passed); the controller's `make golint-quiet GO_VERSION=1.27.4` then passed (97.8 s) |
| 6 | `make test-go` and `make test-frontend` (failing first), `make generate`, `make golint-quiet`, `make check-frontend-quiet`, `make test-quiet`; fix round: `make test-go`, `make test-frontend`, `make check-frontend-quiet`, `make golint-quiet GO_VERSION=1.27.4`, `make test-quiet GO_VERSION=1.27.4` (2051 frontend tests) | pass in the fix round. In the first round `make golint-quiet` found 13 issues, 12 its own (fixed), and then one left in Task 5's `runScheduledPass`, on which `make test-quiet` failed in its lint lane only (every other lane passed); Task 5 fixed that finding |
| 7 | `make test-frontend` (failing first), `make check-frontend-quiet` (2060 tests); round 2: `make test-frontend` (a deliberate undo of three fixes shown failing); its lint and type check ran in Task 8's gates | pass |
| 8 | `make test-go`, `make test-frontend` (failing first), `make generate`, `make golint-quiet GO_VERSION=1.27.4`, `make check-frontend-quiet`, `make test-quiet GO_VERSION=1.27.4`, `make lint-quiet`, `make tsc-quiet`, `make compiler-coverage` (no bailouts), `make test-frontend-browser-quiet` (112 passed, 1 skipped); round 2: `make check-frontend-quiet` (2101 tests), `make golint-quiet GO_VERSION=1.27.4`, `make test-quiet GO_VERSION=1.27.4` | pass |
| Final fix round (from its report) | `make test-go` per package (failing first, then 9 packages pass), `make test-frontend` (the settings page, 20; the invalidations, 24; failing first), `make test-frontend-browser PLAYWRIGHT_ARGS=src/test/browser/schedules.spec.ts` (failing first, then 4 pass), `make golint-quiet GO_VERSION=1.27.4` (two complexity findings in its tests, fixed, then pass), `make check-frontend-quiet`, `make test-quiet GO_VERSION=1.27.4` (313 files, 2103 tests, no dead code), `make test-frontend-browser-quiet` (113 passed) | pass |
| 9 | `make test-quiet GO_VERSION=1.27.4` (313 frontend files and 2101 tests; every Go package from the test cache, no Go change since Task 8's run); `make vm-reset`, `make vm-install`, `make test-vm-browser-quiet PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5b.spec.ts'`, three times (below); `make vm-test-btrfs-integration`; `make check-frontend-quiet` after the spec's last changes (313 files, 2101 tests); `make check-docs-quiet`; `make vm-reset` | the gates pass; VM run 3: 11 of 11; the btrfs tests pass |
| 9, final round | part 1: `make check-frontend-quiet` (2103 tests), `make check-docs-quiet`; part 2: `make test-quiet GO_VERSION=1.27.4` (313 files, 2103 tests, no dead code), `make vm-reset`, `make vm-install`, `make test-vm-browser-quiet PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5b.spec.ts'` (run 4), `make check-docs-quiet`, `make vm-reset` | pass; run 4: 11 of 11 in 4.7 min |
| After the last reviews (the controller's own changes) | `make test-frontend VITEST_FILE=src/api/operation-query-invalidations.test.ts` (the delete's bell refresh); `make test-go` on `./bridge/handlers/alerts ./bridge/handlers/schedules ./common/alerts` (failing first: the kept copy on a refused save, the 409 for stored settings validation refuses); `make golint-quiet GO_VERSION=1.27.4`; after moving the kept-copy warning behind the write: `make golint-quiet GO_VERSION=1.27.4`, `make test-quiet GO_VERSION=1.27.4`, `make check-docs-quiet` | pass. Run 4 in the VM ran on the tree before the warning moved behind the write; that move changes when one journal line is written, nothing else |

**Checked in the test VM** (Ubuntu 26.04.1 LTS, kernel 7.0.0-38-generic,
systemd 259.5, as `lsb_release -ds`, `uname -r` and `systemctl --version`
printed over `make vm-ssh` after run 3; each run after `make vm-reset` and
`make vm-install`):
`make test-vm-browser-quiet PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5b.spec.ts'`
runs `frontend/src/test/vm/jobs-phase5b.spec.ts`, signed in as the VM's sudo
user. Each check starts from settings of its own (it deletes
`notifications.json` or writes the file it needs) and marks every alert seen
through the bell before it counts unseen ones. Webhook posts go to a listener
the spec installs in the VM (`e2e-webhook.socket` on `127.0.0.1:18089`, a few
lines of `python3` recording each body), so nothing leaves the VM. Run 1
(07:15): 6 of 11 passed, run 2 (07:19): 10 of 11, both failing on faults of
the spec (below). Run 3 (07:26): **11 of 11 passed**, in 3.5 minutes, with
the earlier spec and before the final fix round; its table follows. Run 4
(11:13, all on 2026-11-11), with the code final and the spec's last version:
**11 of 11 passed**, in 4.7 minutes; its table follows run 3's. The results
are the runs' Playwright output; what the run 3 table adds from the VM's
stores and journal was read over `make vm-ssh` after run 3, and is marked so.

| Check | Result (run 3: the earlier spec, before the final fix round) |
|---|---|
| The grid on the page, and a save's file | pass. With no file, *Where each level goes* is a table (`display: table`) with column headers *Browser*, *Email*, *Webhook* and row headers *Notices*, *Warnings*, *Alerts*; its nine boxes are laid out as a grid (a column's boxes share their centre within 1 px, a row's likewise, in the headers' order) and show the defaults: every level in the browser, alerts also by email and webhook. *Alerts in the browser* is ticked, disabled and described by "Alerts always show in the bell.". Saving *Jobs started in the browser* as *Also long jobs that finish* stores `report.browser_jobs: long_jobs`; the file and its lock are then `600 root:root` |
| A script that reports every run | pass. With *Scheduled scripts* at *Every run* (stored), a completed `echo` run raises `jobs/finished-<schedule id>`, notice, *`<name>` finished*, linked to its schedule, count 1; after a reload the badge reads 1 and the tray lists one line with "It ran for …"; opening the tray takes the badge away. A second run adds no line: the record's count is 2, the badge reads 1 again and the line reads ×2. Back at *Failures only* (stored), a third run leaves every member of the record as it was, and `alerts.list` answers no unseen alert and the line as seen, count 2 |
| A schedule's own choice | pass. With the kind at *Failures only* (no file), the dialog of schedule A shows *Use the default*; saving *After every run* stores `scripts: {A: every_run}` and leaves A's service and timer files, definition and script (inode, size, time and content) and its timer's state as they were; the file the dialog created and its lock are `600 root:root`; the dialog then shows *After every run*. A's run raises `A finished`; schedule B's run raises nothing; the tray lists A's line and not B's. While a `sleep 60` script runs, saving its choice is accepted (in this run the dialog was opened from the list with nothing selected, because of the layout fault below; run 4 opens it with the schedule selected): its files and timer state are unchanged, its unit keeps the same invocation, its record stays `running`, the choice is stored beside A's, and its end raises its `finished` notice |
| A level kept from the bell | pass. With *Notices in the browser* unticked (stored `info` with no channel) and scripts at *Every run*: a completed run raises its notice into the store, which `alerts.list` does not list; with a failed run beside it the badge reads 1 (the failure only) and the tray lists the failure and not the notice. Ticked again, the notice is listed as unseen (mark all seen had skipped it); after it was seen, the next run makes the badge read 1 and the line ×2 |
| An older settings file | pass. A file holding only `minSeverity: warning`, a webhook URL and `dockerUpdates: on_change`, written over SSH: the page ticks every level for the browser, warnings and alerts by webhook and by email, notices by neither; *Docker updates* reads *Also when something changed*; the webhook URL is shown; the file's SHA-256 is unchanged after the page read it |
| An unreadable settings file | pass. Over `{not json`: the page shows "The stored notification settings could not be read. Saving replaces them."; *Send test* is disabled with the title "Save first"; *Save* is enabled without an edit and writes the defaults (a valid file, `600 root:root`), after which the warning goes without a reload and *Send test* is enabled. The root job host, which runs the save, logged `notification settings unreadable, starting from the defaults: parse /var/lib/linuxio/notifications.json: not valid notification settings at offset 2` (the spec's journal read) |
| The grid picks the channel | pass. With the webhook URL saved and only *Alerts by webhook* ticked, and scripts at *Every run*: a failing script (`exit 5`) is posted once, `severity: error`, `source: jobs`, message "exited with status 5"; a completing script's notice is in the store and is not posted; no `alert delivery failed` in the journal |
| An interruption by a reboot is a warning | pass. Two power cuts during a `sleep 300` run, each followed by a sign-in on a new page; the root job host marks the run `interrupted` with reason `reboot` and raises *`<name>` was interrupted* as a **warning**, "The system stopped before the run finished.". With only *Alerts by webhook* ticked, nothing is posted for it within 20 s of its raise (delivery is bounded at 15 s and logs nothing when it sends nothing), no `alert delivery failed` is logged in that boot, and a probe posted afterwards is recorded. With *Warnings by webhook* ticked, the next interruption is posted once with `severity: warning`. The tray shows both lines with the warning tile (`--app-palette-warning-main`). The power cut was chosen over an orderly reboot because the interruption is then raised at boot, after the listener's socket exists; in an orderly reboot the end hook raises it while units stop, the listener among them |
| The notice at sign-in | pass. With every alert seen and one failed run since, after signing out and in: the toast "While you were away: 1 new notification"; its *Show*, focused and pressed with Enter, opens the tray and leaves keyboard focus on the bell's button (the first time this was seen outside jsdom); the alert is then seen in the store (the user's `seen` count is 1) and the badge is gone. After a reload, with the alerts listed in the tray, no toast |
| A stored `never` for system updates | pass. The fresh VM reads `notify: never`, `failures_unreported: true`, `can_configure: true`, offered `on_failure`, `on_change`, `always` (printed by the spec). The page shows "System update failures are not reported on this machine." and *System updates* reads *Not reported*, its list offering only *Failures only*, *Also when something changed* and *Every run*. *Report failures* saves; the warning goes, the select reads *Failures only*, the drop-in `/etc/systemd/system/apt-daily-upgrade.service.d/linuxio-alert.conf` holds `--policy on_failure` (directory `700 root:root`, file `644 root:root`), and the state reads `on_failure` with no flag |
| `login.spec.ts` | pass |

Read over `make vm-ssh` after run 3: the listener recorded the two posts above
and three probes, nothing else; the jobs alerts in the store were the ten the
checks raised, with the levels and counts the table gives; the boot after the
second power cut logged no `alert delivery failed`.

The spec changed between the runs without weakening an assertion. Run 1
failed five checks on two faults of the spec: it marked alerts seen through
`alerts.mark_all_seen` called on a WebSocket of its own, which the bridge
refuses (`job ID must be a canonical non-zero UUID`: that route is a mutation,
and the spec's helper sends no job ID), so it now opens the tray as a user
does; and it read the APT drop-in without `sudo`, which its `0700` directory
refuses. Run 2 failed the choice saved while the script runs: the row's
*Edit* was not there for 59 s (the trace's own log), because while a selected
schedule's script runs the page's live output takes its height and the
schedules table renders no rows (seen on the way, below); for run 3 the spec
opened the dialog from the list with nothing selected.

| Check | Result (run 4: the spec's last version, the code final) |
|---|---|
| The grid on the page, and a save's file | pass (4.6 s), as in run 3 |
| A script that reports every run | pass (14.2 s), as in run 3, and further: with the tray listing the schedule's `finished` line, deleting the schedule (its row's *Delete*, then *Delete* in the confirmation) removes `jobs/finished-<id>` from the store, and without a reload the tray no longer lists the line within 10 s, well inside the bell's one-minute poll |
| A schedule's own choice | pass (1.2 min), as in run 3, except that while the `sleep 60` script runs the schedule stays selected: the URL selects it, *Output of the latest run* is shown, and its row in the schedules table is in the viewport at 1280×720 (the layout fixed in the final fix round, in the real app); the dialog is opened from that row, the save is accepted, the files, timer state and invocation are unchanged, and its end raises its `finished` notice |
| A level kept from the bell | pass (10.4 s), as in run 3 |
| An older settings file | pass (3.2 s), as in run 3 |
| An unreadable settings file | pass (4.6 s), as in run 3, and further: exactly one `notification settings unreadable, starting from the defaults` line in the journal (from `linuxio-jobs@0.service`), and `notifications.json.unreadable` is `600 root:root` and holds the old bytes `{not json` |
| An interruption by a reboot is a warning | pass (1.2 min), as in run 3, with the apt timers stopped (and any apt run waited for) after each power cut. A power cut gives reason `reboot`, which stays a warning after the final fix round |
| The notice at sign-in | pass (1.2 min), as in run 3, and further: after the toast was handled, the same schedule fails again; the bell's own poll shows the badge `1`; after a reload, with the badge `1` again (the list the notice counts has loaded with something unseen), no "While you were away" toast |
| A stored `never` for system updates | pass (6.3 s). The check first removes the APT drop-in over SSH and runs `daemon-reload`, and skips only when the tool cannot be configured or automatic updates are off; the VM read `enabled: true`, `notify: never`, `failures_unreported: true`, `can_configure: true`, which the check now asserts. The rest as in run 3 |
| `login.spec.ts` | pass (12.5 s) |

Run 4 did not read the VM's stores or journal beyond what the spec itself
reads, and covered no orderly reboot, no crash of a job host (whose
interruption is now an alert), no refused page save over an unparsable file,
no 409 for a schedule's choice, and no Docker, email or DNF/Mint path; those
rest on unit tests (below).

**Root btrfs tests in the VM** (`make vm-test-btrfs-integration`; Task 4
changed what `RunReplication` returns): pass, after run 3 and before the
last reset, on kernel 7.0.0-38-generic: `TestBtrfsIntegration`,
`TestBtrfsDegradedIntegration`, `TestBtrfsResumeIntegration` (its 13
subtests) and `TestPartitionIntegration`, nothing failed or skipped. None of
them calls `RunReplication` or `replicate`; `TestBtrfsIntegration` streams a
snapshot through the kernel's send ioctl. So this run shows the btrfs code
still works against the kernel, not that the new `sent` count is right on a
real send: that rests on `TestRunReplicationCountsWhatItSent`, a unit test.

**Covered by unit tests only** (not run in the VM): the email channel (a fake
SMTP server); DNF and Mint, their two policies and an older `on_change`
drop-in (no such VM); the Docker producers, the pass notice and
`linuxio-update-pass` (not exercised in this spec); the `snapshot-run`
notice and `resynced-` following `resync`; the long-job notice and the
file-job titles; an older page's save without `levels` or `report`; the
other `minSeverity` values of an older file at delivery
(`TestDeliveryFromAnOlderBuildsFile`); eviction by level; the notice at
sign-in for a user without sudo and `historySeenAt`'s validation; the
Schedules page over an unreadable settings file; an interruption by an
orderly reboot raised as a warning by the end hook (the VM checked the
power-cut path).

**Read from the source only, or not checked.** The end hook's privileges on a
real unit were not checked on their own; the VM shows only that it reads the
root-only `notifications.json` (the `finished` notices followed the stored
choices). No automatic update ran in the VM, so the hook *Report failures*
installs was not seen raising anything. The test notification was not sent.

**Privilege review** (the `privilege-boundary-reviewer` on the whole backend
change after Task 8, run by the controller): no Critical finding; nothing
widens root for an unprivileged request. Every alerts and schedules route is
`Privileged()`, and no unprivileged route reads or writes
`notifications.json`; the SMTP password is never returned, logged or put in
an error; the file is written `0600` by an atomic replace under a root-owned
directory, with no request value in a path and schedule IDs validated before
they become map keys; only root processes read it (a user's job host uses the
defaults in memory); an error can never be kept from the browser;
`historySeenAt` goes only to the caller's own config; alert sources are
constants and keys are IDs or constants. One Important finding predates this
work (below). Its minor findings are among the open items.

**Open, for the owner:**

1. **Four update routes without `Privileged()`** (pre-existing, from the
   privilege review): `updates.set_auto_updates`,
   `updates.apply_offline_updates`, `updates.refresh_cache`
   (`backend/bridge/handlers/packages/handlers.go`) and `packages.update`
   (`package_update_operation.go`) need root but do not declare it, so a
   session without sudo fails part-way with a permission error instead of a
   403. No escalation: the bridge runs as that user.
2. **The Go release.** `backend/go.mod` pins Go 1.27.3, on which the
   repository's lint lane fails since GO-2026-6859 was published; Task 5's
   second fix round met it first, and every `make golint-quiet` and
   `make test-quiet` that passed after it ran with `GO_VERSION=1.27.4`.
   Moving the pin is the owner's dependency decision; a separate
   `chore(deps)` commit is recommended.
3. **Two alert gaps** that predate this work and sit badly with "failures are
   always reported": a timer pass that fails as a whole without a failure of
   one of its items (Docker not reachable, an unreadable rules or jobs file, a
   scheduled container that is gone) is recorded as failed and raises no
   alert; a container whose update check fails raises no alert (status file
   and *Scan failed* chip only) and has its "update available" alert cleared
   ([Alerts](../alerts.md#limits)).
4. **Behaviour changes to state in the release notes**: a stored Docker
   `never` no longer silences failures; an interruption by a shutdown or a
   reboot is a warning (it was an alert), which the grid can keep from the
   bell, email and webhook, so on a host that had "Errors only" it is no
   longer emailed unless *Warnings* is ticked for *Email* (a crash, and a job
   that could not be resumed, stay alerts); a save from the page over an
   unreadable `notifications.json` writes a file without the per-schedule
   choices and the SMTP password, and keeps a copy of the old one as
   `notifications.json.unreadable`, which the admin removes by hand.
5. **Deferred minor findings** of the reviews, not fixed: while the file
   does not parse, a delete of a schedule with no choice logs "could not
   clear"; a hand-edited invalid `minSeverity` or `dockerUpdates` in the
   stored file makes a schedule's choice fail (409, naming the settings page
   as the repair) until a page save repairs it; the schedule dialog gives no hint when a choice
   has no effect; a user without sudo who signs in while an update waits
   loses that sign-in's notice; mark all seen lists
   and marks under separate locks (predates); `alerts.mark_seen` with
   explicit IDs is not filtered by the grid; a notice raised into a store full
   of undismissed alerts evicts the oldest alert; a long job's last frame
   waits for its notice's delivery, and a resumed job's run time includes the
   downtime; the snapshot pass's summary prints `replicated=0 sent=1` when
   recording fails after a send; a container named `linuxio-update-pass`
   would share its failure key with the pass notice, and the existing Docker
   keys already overlap (`updated-run` against a container named
   `updated-run`); a choice-only schedule update skips the re-apply that
   repairs an incomplete schedule (a second save repairs it);
   `changeLocked` takes six positional parameters; the *Scheduled scripts*
   help line is not tied to its select for assistive technology, and both
   page warnings use `role="alert"`; after a failed first history load, a
   non-admin's notice observer refetches on focus (its result ignored); a
   bare `\r` in a message is not normalised in the email body
   (pre-existing). Fixed in the final fix round, of the earlier list:
   `SaveNotifications` (removed), the warning per poll (now debug), the
   comments on interruptions, the webhook error's URL path (host only now)
   and `historySeenAt` (normalised and clamped by the bridge).

Seen on the way:

- While a selected schedule's script ran, the Schedules page's *Output of
  the latest run* took the page's height at 1280×720 and the schedules table
  rendered no rows, only its header; the table came back when the run ended
  (run 2's trace, two screencast frames 56 s apart). A layout fault of phase
  4's page; fixed in the final fix round (Ruling 25: the details area is at
  most half the page and scrolls), with a real-browser test at 1280×720 in
  `frontend/src/test/browser/schedules.spec.ts`. The VM spec checks it
  too, and run 4 saw the table keep its rows in the real app.
- A settings save is a mutation, so it runs in the root job host: its warning
  over an unreadable file is in `linuxio-jobs@0.service`'s journal, not the
  bridge's.
- The session bridge logged `route failed: write frame: i/o deadline
  reached` twice during run 2 (07:20:10 and 07:21:14), the second as the
  `sleep 60` run ended. Not looked into.

## 4. Left to do

### 4.1 Open decisions

None. How a timer reaches the job store was decided on 2026-11-10 (D8, section
2.14).

### 4.2 Phase 2: every mutation is a job

Phase 2 moves all the mutations at once rather than the long ones first: a
split would rewrite the same call sites twice and keep both task systems alive
in between. Each step below is one commit that leaves `make test-quiet` green.

**Step 1: prerequisites.** What every mutation would hit once it runs in a host. Done (`645134bf`).

1. **Handlers act for the job's user.** Handlers read the user and the user's
   config store from the `runtime.Runtime` they were registered with, and the
   root host registers them once, as root. It would run every admin's job as
   root's user: a Compose action resolves root's Docker folders, a file
   operation gives new files to root, a setting lands in root's file. The root
   host therefore builds a router per bridge connection for the hello's user,
   with that user's config store, as the privileged bridge does; the hello and
   the record's owner gain the gid, and a resume builds one for the record's
   owner. A user host keeps its single router, since its only peers are its own
   uid. The task service stays shared, so admission, `tasks.*` and cancel do
   not change.
2. **Config reads see other processes' writes.** The bridge and the host each
   keep the user's settings in memory and reread the file only to write it, so
   a setting changed by a job would never reach the bridge's `config.get`; two
   logins of one user already disagree this way. `Snapshot` and `UISnapshot`
   reread a file that changed on disk, which is safe because writes are atomic
   renames.
3. **The behaviour gaps of section 5, fixed:**
   - An oversized result is dropped from the record instead of leaving it
     `running`.
   - A zombie executor counts as dead.
   - A job admitted as the host stops is interrupted like the running ones.
   - A record a host left `running` with no task to finish it is marked
     interrupted when a resend or `tasks.*` asks for it. Records no host ran,
     such as the root bridge's network handoffs, are left alone.
   - `tasks.get`, `tasks.watch` and `tasks.cancel` of a job no longer in memory
     are answered from its record. Through the bridge they wait up to 10 s for
     a host that is reconnecting, then answer 503 instead of 404.
   - When the bridge reopens its host's `tasks.events`, it names the jobs it
     saw running, and the host sends the end of those that ended meanwhile
     (final review, Important 3).
   - A resend and a reattach each get one try, so a loss after a resend still
     reattaches (final review, Important 4).
   - A host prunes daily as well as at start, and the 200-record cap is per
     store, not per admin.
   - The shutdown drain also waits for `tasks.watch` streams.
   - Found while building item 1: same-target admission was counted per
     router, so two admins' routers on the root host would not refuse each
     other. The counts moved to the shared task service. Suspend and resume
     hooks also act for the record's owner.

   Unchanged: a forwarded mutation whose host drops still ends with a bare
   close; the client takes it for a lost connection and reattaches, which now
   works. `cancel_requested` moved to phase 3: it matters for resume, and the
   record staying `running` until the handler returns needs a canceling state
   in the UI. Accepted: a job that completes in under a second keeps no record,
   so a resend after the 30-minute memory sweep runs it again; the browser
   resends only right after a start it lost.
4. **No default deadline.** A server-side timeout for quick mutations was
   considered and dropped: it brings back the fast/slow decision per route
   that section 2.1 removes, and turns a slow change into a failure part-way.
   A stuck job stays in the tray, where it can be cancelled.

**Step 2: the routes, family by family.** One commit per batch of families,
each with its backend routes, `make generate` and its frontend callers, since
a route that becomes a `Mutation` no longer type-checks in `useCallMutation`.

- Every mutation route of the map becomes `Mutation` or `MutationVoid`
  (section 2.17). `TaskRunner` runners take `ctx` and report through
  `bridgeipc.ReportProgress`; `WithTaskMetadata` identities become `Target()`
  on the request type.
- Stay in the bridge: the three `BrowserBound` routes, and any route whose
  effect is the bridge's own process state (a cache only the bridge reads, the
  session itself). Each batch checks its family for these and lists them here.
- The nine self-severing routes run in the host like the rest: the change
  completes there and only the reply is lost.
- Fixed when their route moves, each with a test:
  - The btrfs and Docker storage fixes moved to phase 3 (section 4.3).
  - The 13 routes missing `Privileged()`: found already declared when batch 2
    moved them.
  - `/etc/default/btrfsmaintenance` and the WireGuard configs: atomic writes.
- Frontend: `useCallMutation(linuxio.<domain>.<name>, config)` becomes
  `linuxio.<domain>.<name>.useMutation(config)` (about 190 calls in 80
  files); the `useTaskStreamAction` callers and the transfer engine in
  `frontend/src/tasks/` move to `.useMutation` with `onProgress`.
- `jobs.Protocol` goes up once, because routes move from the bridge to the
  host: an older host answers a newer bridge `update_pending` and exits when
  idle instead of running a route as a `Call`.

Batches:

1. **Done** (`d17eef19`). The 92 writes of the accounts, alerts, config,
   control, datetime, hostname, indexer, monitoring, network, power,
   schedules, shares, system, systemd, updates, virt and WireGuard families
   are `Mutation` or `MutationVoid`, and their 94 callers use `.useMutation`;
   `jobs.Protocol` is 2. The account writes declare `Privileged()` (they run
   `useradd`, `chpasswd`, `passwd` and `gpasswd`), and the WireGuard configs
   are written atomically and `0600`. No route of these families changes state
   that only the bridge process holds. Left as Calls:
   - `logging.set_debug`: it also switches the calling bridge's own log level.
     Step 3 decides between a bridge-local `Mutation` and making each process
     follow the setting.
   - `network.start_bridge_handoff`, `network.confirm_bridge_handoff` and
     `network.revert_bridge_handoff`: already durable through their own record
     and native rollback; they move with their dialog.
   - The reads `network.get_network_info` and `system.get_health_summary`.

   Tests that mock `useCallMutation` reach the new hooks through
   `frontend/src/test/mutationHooks.ts`. A non-admin who logs in from the
   server's own browser gets a local logind session, where polkit allows a
   PackageKit refresh and the offline-update trigger without a password; a job
   host is a service, which polkit asks for one. Remote logins are
   `Remote=yes`, which polkit already treats like a service.
2. **Done** (`e92e3161`). The 70 remaining writes that were plain Calls,
   in the Docker, file browser and storage families, are `Mutation` or
   `MutationVoid`, and their callers use `.useMutation`;
   `/etc/default/btrfsmaintenance` is written atomically. Every Docker write,
   `docker.compose` included, declares `Privileged()`: by security policy the
   `docker` group has no members, so only a sudo-authorized login reaches
   Docker. Left as
   Calls: `storage.btrfs_replication_ssh_key`, a get-or-create read the UI uses
   as a query, and the four cache refreshes of section 2.17.
3. **Done** (`2170f6bf`). All 31 task routes are Mutations: 28 run in
   the job host, and `filebrowser.upload`, `filebrowser.upload_batch` and
   `filebrowser.archive` stay in the bridge with `BrowserBound()`. Their
   runners are unchanged: `apischema.MutationTask` hands a runner its task from
   the context. They keep their task metadata, which the tray, recovery and
   the transfer engine read, and the five that ran one at a time
   (`control.app_update`, `docker.apply_daemon_storage`, `packages.update`,
   `system.install_capability`, `filebrowser.index`) declare
   `apischema.Exclusive()`. The frontend keeps its task client and its callers:
   the generator still emits these routes as `"task"` with
   `createTaskEndpoint`, and the client starts each over the mutation stream
   with a job id (`startJob`), takes the first snapshot, detaches and follows
   with `tasks.watch` and `tasks.data` as before. `control.app_update` records
   the admin who started it rather than the host's uid. Moving the
   frontend's task hooks onto `.useMutation` is step 3.

**Step 3: delete the old task code.** Done (`d2871872`).

- Backend: `apischema` loses `TaskRunner`, `SessionTask`, `DurableTask`,
  `WithTaskLifetime`, `WithTaskProgress`, `WithTaskIdentity`, `KindTaskRunner`
  and the task binding path; `bridgeipc` loses `ModeTask`,
  `Router.TaskRunner`, `RecoverDurableTask`, the 25 ms settle start and
  per-route task identities. A Mutation's lifetime is set at registration:
  durable, or session for `BrowserBound()`. The guards that checked only Task
  routes are gone; admission, identity, disconnect and redaction tests run on
  Mutations.
- Frontend: the generator emits the 31 routes with `defineMutation`, and
  `RouteMode` has no `"task"`. `task-react-query.ts` (`createTaskEndpoint`,
  `useTaskAction`, `useTaskStreamAction`) is deleted. Components use
  `.useMutation`, which gained `signal` and `closeOnAbort: "close"` to detach
  (Compose, SMART tests, package updates); the tray engines and uploads start
  jobs with `endpoint.start`, which resolves with the first snapshot and
  detaches, and follow them with `tasks.watch` and `tasks.data` as before.
- `useCallMutation` stays: its callers are Calls run as commands (update
  checks, config downloads, `tasks.cancel`, folder validation), not writes.
- `logging.set_debug` stays a Call, and a job host starts at the debug level
  it persisted (it always started at Info before).

### 4.3 Phase 3: storage safety and btrfs resume (done)

Done in `938e32c5` and `f8db05af`; section 3.6 describes what was built and how
it was checked.

- `storage.btrfs_device_remove`: cancel issues the kernel cancel instead of
  only marking the task.
- `storage.btrfs_pool_create`: on failure, undoes the partitions it created
  and wipes the superblocks it wrote.
- `docker.apply_daemon_storage`: stops the containers, writes the files while
  Docker still runs and asks systemd for one restart of `docker.service`,
  which systemd finishes even if the job host dies. Nothing stops Docker or
  `docker.socket` as a step of its own, so a dead job cannot leave Docker down
  (D1). The confirm dialog warns that a reverse proxy, tunnel or VPN running
  in Docker goes away until its stack is deployed again.
- `Resumable` hooks for `storage.btrfs_scrub_start`, `storage.btrfs_balance`,
  `storage.btrfs_device_replace` and `storage.btrfs_device_remove` (section
  2.11), with kill-and-resume steps in `make test-btrfs-integration` and the
  VM checks of section 3.6. Cancel writes `cancel_requested` before it
  cancels, and the record turns `canceled` only when the handler returns, so a
  cancelled job is never resumed.
- A pool a power cut left mid-replace is mounted at boot by
  `linuxio-btrfs-degraded`, so the kernel continues the replace.

Moved out of phase 3 (not lost):

- The submit CLI and the host's resume call (D6) were removed in Task 12;
  phase 4 designs how timers reach the job store (section 4.1).
- Periodic scrub checkpoints: a power loss rescans from 0.

Three more moved out and were done afterwards: the "canceling" state in the UI
and the progress for device removal (section 3.7), and the overview and the
bridge's offline-pool mount counting a replace target as a present disk
(`4d73aac5`, section 3.6).

### 4.4 Phase 4: schedules and system jobs (done)

Section 2.14 is the design and section 3.8 what was built and checked, task by
task as in the
[phase 4 plan](../superpowers/plans/2026-11-10-jobs-phase4-scheduled-runs.md):
the scheduled store and its limit, the two unit hooks and the output copy,
progress from the btrfs timer program and the Docker update runner, the root
host's listing and liveness check, the bell rows and the live output on the
Schedules page.

Left open by the phase, none of them blocking:

- Listing ended runs and reading their saved output belong to the history
  view (phase 5, section 4.5).
- A run shorter than one poll (2 s) never shows in the bell. A run that ends
  while the bridge reconnects to the root host gets no terminal event on that
  stream; its row goes at the next reconnect.
- A stop or scrub cancel that fails takes its `cancel_requested` mark back;
  when that withdraw fails too (logged at Warn), the flag stays, and a run
  that later fails on its own reads `canceled`.
- The mark is one flag per run, so two requests can cross. Two scrub cancels
  at once for one pool: when the second fails, its withdraw clears the first
  one's mark and the resync reads `failed`. A stop that reached systemd but
  whose reply was lost withdraws the mark, so the run the user stopped reads
  `failed` and a script raises its alert. A run that ends on its own between
  the mark and a failed stop reads `canceled`.
- A record that `end` cannot write gets no alert from `end`. When `begin`
  wrote it, the root host's liveness check later finds the run gone, marks it
  `interrupted/crash` and raises the alert ("The run ended without reporting
  its result."); only a run neither hook could record goes unseen.
- Every listing reads every record of the scheduled store, per listening
  client, every 30 s and after each store change. Fine at 50 runs per unit.
- The Docker update message names the container but no count, since a pass
  does not know up front how many containers need an update. A resync's soft
  convert reports no progress of its own.
- A fresh install of an older release fetches `packaging/` from `main`, but
  its installer does not install the drop-in. Only the resync unit
  (`linuxio-btrfs-resync@.service`, fetched from `main`) then calls
  `linuxio scheduled-run`, which the older CLI answers with "Unknown command";
  the `-` prefix makes systemd ignore that, at one failed hook line per run.

### 4.5 Phase 5: history and notices

The history view, the "ended while you were away" notice at login, the
"waits for N jobs" update banner (section 2.16).

Plan 5a (the history page, each schedule's past runs, the saved output viewer
and the update banner) is built; section 3.9 says what was built, how it was
checked, the three faults the test VM found and the limits. Plan 5b (the
notification settings and the notice at sign-in) is built, not yet committed;
section 3.10 says what was built, how it was checked and what is left open.

Decided with the user on 2026-11-10, before the plan is written:

- **History.** A "History" link at the bottom of the bell opens a page that
  lists ended jobs and scheduled runs, newest first. Each schedule on the
  Schedules page also lists its own past runs with their saved output.
- **Who sees what.** Admins see everything, scheduled runs included. Other
  users see only the jobs they started.
- **Quick actions.** A job that succeeds in under a second still leaves no
  history entry.
- **Update banner.** "Update waits for N jobs", with a link to the bell and no
  button to force the update: forcing means stopping the old job host, which
  interrupts the jobs it runs, and the user wants a button only if it breaks
  none. Letting the new host start beside the old one, so that new jobs never
  wait, is the way to give that; it is a design change of its own and is not
  part of phase 5. As built, the banner names the jobs itself instead of
  linking to the bell, because the bell cannot show a job of an older host
  (section 3.9); the user asked to revisit that.
- **Saved output** is raw text with control characters kept (phase 4). The
  view shows it as plain text and stays behind the admin gate.

Decided for plan 5b, and built (section 3.10):

- **Notifications.** Decided on 2026-11-10: "fully configurable like Unraid",
  in two parts.
  - *What to hear about*, one line per kind of work: system updates, Docker
    updates, scheduled scripts, snapshots and replication, pool resync, each
    with "failures only" (the default), "also when something changed" or
    "every run"; storage health (missing disk, errors, low space) is always
    on; jobs started in the browser are one line, "failures only" or "also
    long jobs that finish" (not one setting per mutation). Failures can never
    be switched off. A schedule's own dialog can override the scheduled
    scripts' line for that script.
  - *Where each level goes*, Unraid's grid: notices (a run completed,
    something changed), warnings (canceled, interrupted) and alerts (failed),
    each ticked for browser, email and webhook; alerts always reach the
    browser. It replaces the single least-severity setting
    (`alerts.NotificationSettings.MinSeverity`).
    As built: a cancelled job or run reports nothing; an interruption by a
    shutdown, a reboot or a power loss is a warning, and one by a crash, or a
    job that could not be resumed, is an alert (below); `minSeverity` is no
    longer read where `levels` is stored, but every save still writes it, as
    the least severe level ticked for email or webhook, for an older build.
  - "While you were away" shows what reached the browser since the user last
    looked.
  - Proposed by the agent, not said by the user: a job is long when it ran
    five minutes or more; the file indexer gets no line.
- **Two plans.** Phase 5 is planned as two pieces, written on 2026-11-10: the
  [history page, each schedule's past runs and the update banner](../superpowers/plans/2026-11-10-jobs-phase5a-history-and-banner.md)
  (built, section 3.9), then the
  [notification settings and the "while you were away" notice](../superpowers/plans/2026-11-10-jobs-phase5b-notifications.md)
  (built, section 3.10).
  The second plan lists the choices the agent made where the code left no
  other honest option: scripts offer "failures only" and "every run" (a
  script has no notion of having changed something); a pool resync offers
  "failures only" and "also when a pool is whole again", and keeps today's
  report by default; a canceled job reports nothing and an interruption is a
  warning; on DNF and Mint only the service result of an update is known;
  a host whose system updates were set to "Never" is told so and offered
  one click; a level unticked for the browser is not shown in the bell.
- **Decided while it was built** (the rulings of the run, each with what it
  costs if wrong; where they differ from the plan's text, they are what was
  built):
  - A kind named in the stored `report` wins over the older `dockerUpdates`
    member, as stored `levels` win over `minSeverity`.
  - The "failures are not reported" flag is
    `AutoUpdateState.failures_unreported`, true while the stored system
    policy is `never`.
  - `/history?job=<id>` for a job outside the loaded entries says so: "This
    job is not among the loaded entries. Load older to look for it." (the
    second sentence only when older entries remain).
  - A settings save with no `levels` sets them from its `minSeverity`, and
    one with no `report` sets the Docker report from its `dockerUpdates`
    and keeps the other stored kinds: that is how an older page says what it
    wants.
  - A save from the settings page replaces a `notifications.json` that does
    not parse, from the defaults, with one warning; the new file has neither
    the per-schedule choices nor the SMTP password, and a copy of the old
    one is kept as `notifications.json.unreadable` (root `0600`, replacing an
    older one; made only when the save is valid and about to write), since it
    may hold the only copy of them. Every other writer refuses it; a
    schedule's choice then answers 409 and names the settings page as the
    repair, as it does over a stored file that validation refuses.
    `alerts.get_notifications` over such a file answers the defaults with
    `settingsUnreadable`, and the page says "The stored notification settings
    could not be read. Saving replaces them."
  - A stored Docker `never` no longer silences failures, from the first save
    by this build on (a save writes `dockerUpdates` as `on_failure`) and in
    the updater, which no longer reads it.
  - A script's "finished" notice is one alert per schedule
    (`jobs/finished-<schedule id>`), each run a new occurrence; and a full
    store with nothing dismissed evicts the oldest alert of the lowest level
    first. One alert per run would have pushed undismissed failures out of
    the 200-record store.
  - A file-browser job's alert is titled `<Action>: <label>` (`Copy: 12 items
    failed`), with *Move to Trash*, *Restore from Trash*, *Delete from Trash*
    and *Change permissions* for the routes whose words do not read as an
    action; other jobs use their label.
  - Liveness reads no settings: it raises only interruptions, and no report
    setting decides whether an interruption is raised; a warning it raises
    then follows the levels grid like any other.
  - Under `every_run` a snapshot pass that failed and took and sent nothing
    raises no "nothing to do" notice; the pass before a package upgrade
    raises no `snapshot-run` notice, only the timer's own pass does.
  - The Docker pass notice's key is `linuxio-update-pass`, not the plan's
    `run`, which a container may be named. Under `every_run` any pass that
    replaced nothing raises it, a Check only pass and an updating one alike
    ("nothing to update", or "N waiting" while the "waiting" alerts are off,
    nothing more while they are on); a pass that replaced a container raises
    only the per-container notices, and one that could not check one of its
    containers raises none.
  - A successful system update that needs a reboot stays a warning, so no
    mail an admin receives today stops.
  - The Schedules dialog sends `notify` only when the user changed it, and
    the stored calendar and arguments when their fields are untouched, so a
    change of only the choice is seen as one.
  - A user with no stored `historySeenAt` gets no notice at that sign-in,
    which stores the baseline; and the time is stored only once the history
    page has shown a loaded history with at least one entry.
  - The settings page keeps its rule that any edit enables *Save*, without
    comparing values.
  - From the final review: an interruption by a crash (the job host died, or
    a script's end hook never reported) and a job that could not be resumed
    after the reboot stay alerts; one by an orderly shutdown, a reboot or a
    power loss is a warning. A crash leaves the outcome unknown, a failure in
    effect, so "failures are always reported" stays whole.
  - No migration tweak for hosts that had "Errors only": after the upgrade a
    run cut short by a shutdown or a reboot is a warning there and is no
    longer emailed unless *Warnings* is ticked for *Email* (or posted unless
    ticked for *Webhook*). Ticking it in the migration would send every other
    warning too.
  - The Schedules page's details area is at most half the page and scrolls,
    as the history page's does, so the table keeps its rows while a selected
    script's live output is shown (a layout fault of phase 4's page, found in
    the VM by Task 9).
- **The bell row of a scheduled run** has an "Open" link, not a clickable
  row (phase 4, shared-component rule). Explained to the user on 2026-11-10;
  it stays unless they ask for the whole row to be clickable.

### 4.6 Every phase

Ends with `make test-quiet`, the documents below updated, and a suggested
commit message: `docs/api-contract.md` (modes, admission, lifetime, tasks
section), `docs/process-systemd-architecture.md` (binaries, units, boot flow),
`docs/bridge_handler_patterns.md`, `docs/btrfs.md` (resume semantics),
`docs/TODO/scheduled-execution.md`, `docs/frontend-task-state-unification.md`,
`docs/TODO/api-reliability-roadmap.md`, `AGENTS.md` if handler guidance
changes, and this document.

## 5. Deferred items

Every item the phase 1 reviews deferred, plus those of the two follow-up
commits. **Open** items were not fixed; those marked *gap* change behaviour
and are listed again in section 4.2. **Fixed** and **obsolete** items are
kept for the record. Statuses were checked against the code where the item
names a concrete line; the rest are as recorded.

### Task 1: store

- [x] *Gap.* Record limit 1 MiB: an oversized result makes `Finish` fail and the record stays `running`. Fixed in phase 2, step 1. Route names are capped at 128 characters (not requested by the spec).
- [x] *Gap.* `ProcessAlive` treats a zombie (state `Z`/`X`) as alive. Fixed in phase 2, step 1. (Its `ParseUint` error now names the pid.)
- [x] `writeFileNoSync` duplicates `atomicfile.Write` without `validateDestination`; `readRegularFile` duplicates the old reader without `filepath.IsLocal`.
- [ ] The fsync rule keys on terminal state, not on a state change; `Finish` on a terminal record still rewrites the file.
- [x] `Finish` measures age from `StartedAt` only; `needsSync` falls back to `CreatedAt`.
- [x] `ReconcileCrashed` with a nil `alive` panics; an invalid reason returns a partial list; `mutate` returns a record together with an error.
- [x] `Prune` swallows the read error without logging. (no change needed: `terminalByUID` already logs it at Warn)
- [x] `WriteProgress` after a transient delete leaves an orphan `.progress` file; progress bytes are not validated as JSON. (a write racing the delete can still leave one; `shortcut:` comment)
- [x] Tests discard errors in `store_test.go`.
- [x] Records with unknown members are no longer pruned (final fix wave). Open part: the comment in `store.go` still says an unreadable record can only be a torn write.
- [x] A non-empty directory named `<uuid>.json` aborted `Prune`: non-regular entries are skipped now. Still inherent: a running job whose record was torn is never alerted after a reboot (no fsync on create).

### Task 2: peer credentials and sd_notify

- [x] No test for the `@` abstract-socket conversion in `Notify` (redundant on Linux, harmless).
- [x] `peercred_test` leaks an fd on a path that is already failing.

### Task 3: declarations

- [x] Redundant derived count in `TestAllTaskRoutesUseTaskRunner`: the test is deleted with the Task mode (phase 2, step 3).
- [ ] The 114 retry-safe count has no replacement beyond the two name lists (phase 2, step 3).
- [x] `TestMutationOptionsAreAllowedOnlyOnMutationRoutes` asserts the conjunction, not each option.
- [x] The `WithTarget` closure is untested.
- [x] `mutationResult` duplicates `wrapTypedCall`'s void erasure.

### Task 4: one stream per mutation

- [x] The `publishTerminal` comment overclaimed ordering (fixed, final wave).
- [x] The `MutationForwarder` doc now says `Forward` owns the error frame (fixed, final wave).
- [ ] A progress frame between subscribe and snapshot can be skipped (`subscribeWithReplayStatus` could return the snapshot under the lock).
- [x] `Started` may see a canceled snapshot when a queued task is cancelled first: the recorder treats a terminal snapshot at `Started` as final (Task 5).

### Task 5: job host

- [x] *Gap.* Admission races SIGTERM: a stream admitted just before cancel can run on with no interrupt. Fixed in phase 2, step 1.
- [x] *Gap.* A running record with no in-memory task is answered as interrupted but stays `running` in the store until the next host start. Fixed in phase 2, step 1.
- [ ] *Gap.* A job completed in under a second keeps no record, so a resend after the 30-minute sweep runs it again (limit of the transient rule; accepted in phase 2, step 1).
- [ ] Progress writes hold `rec.mu`.
- [x] A progress-write error is logged at Debug only.
- [x] A pending progress `AfterFunc` can outlive `Serve`.
- [ ] The owner username comes from the bridge's hello, not `cfg.Username` (the root host needs the hello's; a user host could use its own).
- [x] The 503 "stopping" path and a hello while closing are untested.
- [ ] `sameJSON` is sensitive to key order (fine for the browser's identical resend).
- [x] An unreadable record answers 500 at Warn. (now Error)
- [x] An invalid id lets the refusal answer 503 or 409 before `Dispatch`'s 400.
- [x] Misleading attributes on the refused-connection log (fixed, final wave).
- [x] "job host started" Info before the host listened (removed, final wave).

### Task 6: launcher (obsolete)

The launcher was removed in `0b44d7d3`; all its items are obsolete: success log level, untested `Disallowed` error leg, "uid 0" prefix on a failed peer read, unbounded NSS steps, a socket wait that only checked existence.

### Task 7: client and submit

- [x] Stale-socket wait on the root path: the client now dials with retries.
- [x] `StartResume` bypassing the Privileged check is documented as root-only by design (final wave).
- [x] The "job record not created" log text also fires on a refused rerun.
- [x] The CLI accepting a malformed `--resume` id: obsolete, the CLI is removed (D6).
- [x] Two test strengthenings: a hello as uid 0 against a user host; `tasks.get` after a no-wait submit.

### Task 8: units and boot reconcile (mostly obsolete)

- [x] Fixtures pin `User=%i`, `StateDirectoryMode=0700`, `NoNewPrivileges=yes` (final wave).
- Obsolete with the reconcile service and launcher (`0c82d2d7`, `0b44d7d3`): hard-coded runtime path in `markUID`; the boot window between launcher and reconcile; a failed boot run re-running later; a record left root-owned after an `fsyncDir` failure; the decorative `TestHandBackCandidatesSkipsStrayFiles` setup.

### Task 9: relay

- [x] *Gap.* A forwarded mutation whose host drops mid-run ends with a bare close. Kept: the client takes the close for a lost connection and reattaches; since phase 2, step 1 the bridge's `tasks.watch` waits up to 10 s for the host to return and the host answers from the record.
- [x] *Gap.* `tasks.get` and `tasks.cancel` of a job id with no host answer 404, not 503. Fixed in phase 2, step 1.
- [x] *Gap.* Events from the window with no host are not replayed to an already open stream (also final review Important 3). Fixed in phase 2, step 1.
- [x] *Gap.* On the root host the 200-record cap applies per admin (drop the `byUID` grouping in `Prune`). Fixed in phase 2, step 1.
- [x] The first failure after a drop logs at Debug, not Warn.
- [x] `Events` waits for EOF instead of returning on `OpStreamClose`.
- [x] The merge test does not exercise concurrent writes. (a `net.Pipe` serialises writes, so it cannot catch a missing lock)
- [ ] The three cmd relay tests skip as root (running them needs an exported test hook in `jobsclient`; the skip message says so). `TestHostServesMutationOverYamuxAndRecords` now runs as a user host under root too.
- [x] The resume owner change in `lifecycle.go` is untested.
- [x] Live snapshots show owner uid 0 on the root host while records show the real uid: documented in the API contract (final wave); the frontend reads only `owner.username`.

### Task 10: first route

- [x] `Target()` drives the same-target refusal (test `TestMutationTargetRefusesASecondRunOnTheSameTarget`; the smoke "same" case confirms it).
- [x] A second concurrent update waited 10 s and failed: it now waits for the lock and reports "Waiting for another update to finish" (final wave).
- [x] `bridge_handler_patterns.md` had no durable mention left.
- [x] No 400 case in `update_mutation_test`.
- [x] Pre-existing `useCallback`/`useMemo` in the Docker update provider could go.
- [x] The details-mode action and the `UpdateCell` chip have no unprivileged-case test.
- [x] A redundant `it.each` state string.

### Task 11: network handoff

- [x] `status()` decodes the checkpoint twice.
- Accepted by ruling: store paths inline instead of package variables; a stale handoff shows as running until the next status poll or start.
- Obsolete: the durable-operations doc naming the deleted package (doc deleted).

### Task 12: frontend

- [x] *Gap.* Resend-then-reattach: after a resend succeeds, a second loss ends in `outcome_unknown` instead of one reattach (also final review Important 4). Fixed in phase 2, step 1.
- [ ] An abort while waiting for the mux after the job started cannot reach the server (no caller passes a signal yet).
- [x] The `sent === false` branch is untested.
- [x] A long comment line in `apiLayering.test.ts`.
- [ ] `watch()` does `tasks.get` then `tasks.watch`.

### Task 13: docs and browser spec

- [x] *Gap.* The host's `tasks.list` and `tasks.get` see only in-memory jobs. Phase 2, step 1: `tasks.get`, `tasks.watch` and `tasks.cancel` answer from the record; `tasks.list` of past jobs is the history view, phase 5.
- [x] *Gap.* Cancel marks the record canceled at once without `cancel_requested` (moved to phase 3, where resume needs it). The daily prune was added in phase 2, step 1. Fixed in phase 3, task 2.
- [x] `jobs.spec.ts` does not assert the password environment variable.
- [x] The records list in the docs omits the handoff's progress step and checkpoint.
- Obsolete with the reconcile service: boot-flow step order and its alert-exception wording; "session_ref recorded as owner" wording (owner is uid and username).

### Final review and fix wave

- [x] Important 3: terminal-event replay after a host crash (same as Task 9). Fixed in phase 2, step 1.
- [x] Important 4: resend-then-reattach (same as Task 12). Fixed in phase 2, step 1.
- [x] The host-side drain does not cover forwarded `tasks.watch` streams. Fixed in phase 2, step 1.
- [x] A doc clause about `owner.username` (D7) could be clearer.

### Commit `0b44d7d3` review

- [x] A user's host that exits during a login stays down until the next login: fixed by socket activation in `0c82d2d7`.
- [x] SELinux exec context, warning level, `TasksMax` sizing, stale docs, start-failure tests: fixed in the same commit.
- [ ] A privileged login skipping the host start has no hermetic test (sudo cannot succeed in the PAM harness).

### Commit `0c82d2d7` review

- [x] A host that cannot start closes the queued connections; a failed resume is alerted and stamped; the socketless host message; wording; a user-host test: fixed in the same commit.
- Accepted: an upgrade from the unreleased dev builds leaves an old host that blocks the new socket until it exits (one-time `systemctl stop 'linuxio-jobs@*.service'` on dev machines; no released build ran a job host).
- Accepted: records written before boot IDs existed are marked `crash` instead of `reboot` (only unreleased builds wrote them; no route was resumable then).
- Accepted: the explicit `After=` on the socket is redundant with systemd's implicit ordering.

### Phase 3 reviews

Kernel facts the phase relies on, read from torvalds/linux master (2026-10-28)
and tag `v7.0` (2026-11-06), and checked in the VM.

- **Signals** (6.19 and later, `937f99c73613`): any pending signal on the
  ioctl thread aborts a scrub or a device replace with `-EINTR`, and so does a
  freeze. An aborted replace ends `CANCELED` and its target is destroyed. Go
  handles every signal (SIGCHLD, SIGURG, ...), so a child exit in another job
  could land on the ioctl's thread. Long ioctls therefore run on a locked
  thread with async signals blocked (`TestBlockingIoctlBlocksAsyncSignals` and
  the `signals` root step, which sends `tgkill` SIGURG to every thread). That
  does not cover a freeze: a suspend to RAM still aborts a scrub or replace on
  6.19 and later.
- **Positive return codes**: btrfs ioctls report refusals as positive values
  (`enum btrfs_err_code`, for example 8 = `BTRFS_ERROR_DEV_EXCL_RUN_IN_PROGRESS`).
  `fsIoctl` dropped them, so a refused remove or balance read as success; they
  are errors now (`TestRemoveDeviceReportsKernelRefusal`, `refusedWhilePaused`).
- **Stale `reloc_cancel_req`**: a removal cancel that arrives after relocation
  ended leaves `reloc_cancel_req` set, and the next relocation is cancelled at
  its start (`chunk relocation canceled on start`). Documented, not worked
  around ([btrfs.md](../btrfs.md#devices-and-profiles)).
- **`BALANCE_CTL` by pointer**: pause and cancel failed with `EINVAL` on real
  kernels. Fixed; the balance cancel never worked before.
- **A cancel racing a pause**: the job's context cancel runs after `Suspend`
  paused the balance, and a `BALANCE_CTL cancel` then deletes the paused
  balance. Pinned by `TestBalancePauseIsNotCanceledByTheJobContext`.
- **Straddling extent**: errors in the extent a scrub's cut falls in are
  counted twice after a resume. Accepted.
- Reviewers' minor items: the resume of a replace did not check the source
  device id, and a cancel of a replace answered `NOT_STARTED` after the
  kernel finished left a job `canceled` while the pool held the new disk.
  Both are fixed (section 3.7). The pre-6.19 `replaceKill` branch, which had
  never run on a real kernel, passes on 6.8 in a second test VM (section 3.7).

### Other open items

- [x] Stored requests kept secrets for the retention period: `virt.create`'s
  cloud-init password, the account, CIFS, rsync and SMTP passwords. Those
  fields are tagged `secret:"true"` and the record leaves them out; a resend is
  compared in the same form. A handlers test fails for a Mutation request field
  named like a secret without the tag.

- [x] The power-loss reboot path (`interrupted/reboot` marked at boot) had only unit-test coverage. The VM spec now cuts the power under a scrub and a replace and checks the `interrupted/reboot` record and the resume (section 3.6).
- [x] The bridge resource limits: a login stays uncapped and the unit and the docs say so (D7).

## 6. Decision log

Rulings made while building phase 1 and the two follow-up commits, with the
cost if they turn out wrong where it was recorded.

- **No agent commits.** The user commits between tasks; review packages are
  built from staged trees.
- **Task order 1–9, 12, 10, 11, 13.** The generator and `useMutation` had to
  exist before the first real route became a mutation.
- **Model policy.** Sonnet for simple, well-specified tasks; Opus for harder
  ones; Fable for every review (Opus when Fable hit its usage limit).
- **Torn records.** A record that cannot be read is skipped with a warning
  and pruned; no fsync on create stays. Narrowed later: records with unknown
  members are kept, so an older binary never deletes a newer one's records.
- **`interrupted` is a task state** in the contract, not mapped to
  `canceled`, because the UI distinguishes them.
- **`Target()` drives admission** when a mutation route has no metadata.
- **A resend of a stored id is answered from the record**, never re-run.
- **Hosts are outside `linuxio.target`** so updates never stop a running job.
- **Store parents are root 0755 with a 0700 leaf**, created by tmpfiles, so a
  root bridge cannot lock user hosts out.
- **A privileged bridge uses the root host** (its effective uid), and the
  record's owner is the real user from the hello.
- **A second update of the same container queues** behind the Docker update
  lock instead of failing.
- **A host stop interrupts live tasks** so attached clients see `interrupted`.
- **One door, no world sockets** (`0b44d7d3`). Per-user processes start only
  through `linuxio-auth` after PAM or from root; the 0666 launcher was
  removed. The verbs on the bridge binary are not a boundary, the sockets are.
- **The host start runs before the PAM session opens**, so `pam_selinux`
  cannot give systemctl the user's context.
- **Host limits:** `TasksMax=8192` (room for 256 jobs each waiting on a
  subprocess tree; below it the Go runtime aborts the host when it cannot
  create a thread), `MemoryMax=2G`, `OOMPolicy=continue`.
- **Socket activation** (`0c82d2d7`) rather than `Restart=on-failure` plus a
  logind check: only a component that outlives the host can start it at the
  moment a bridge connects, and systemd already is one. The per-user socket is
  `0600`, owned by the user, created only after their PAM login, and stays
  until reboot.
- **No verbs for systemd entry points.** The host is chosen by the socket name
  systemd passes; the reconcile moved into the host.
- **Lingering bridge rejected.** Keeping the bridge alive after logout until
  its jobs end would need no new unit, but it ties jobs to the bridge process
  (no crash isolation), dies with every update, holds a login slot per
  lingering bridge (16 in total, a local denial of service by any user), needs
  the store as a polling bus for the next login, and still needs another
  executor for timers and the boot resume.
- **No compatibility code for unreleased builds.** The jobs commits exist
  only on the dev branch; v0.52.0 has no job hosts.
- **Phase 2 moves every mutation at once** (2026-10-27), instead of the long
  routes in phase 2 and the quick ones in phase 3: one migration per call
  site, and one task system at a time. Resume hooks became phase 3.
- **No default deadline for mutations** (2026-10-27): see section 4.2,
  step 1, item 4.
- **`Exclusive()` for five routes** (2026-10-28), against "no per-route cap"
  in section 2.2: `control.app_update`, `docker.apply_daemon_storage`,
  `packages.update`, `system.install_capability` and `filebrowser.index` ran
  one at a time as tasks, and two overlapping runs would race on binaries,
  Docker's data directory, the package manager or the index. Their targets
  could not express it: two have none, and the others' identities are the
  tray's dedupe keys. On the root host the rule spans every admin.
- **Task client kept for the 31 former task routes** (2026-10-28): the
  frontend starts them over the Mutation stream and follows them as before,
  so batch 3 moved them into the job host without rewriting the tray, the
  transfer engine, recovery and uploads; step 3 decides their hooks.
  Superseded in step 3: components use `.useMutation`, the tray uses
  `endpoint.start`.
- **`useCallMutation` kept** (2026-10-28), against section 2.16's "deleted
  after migration": every write is a Mutation, and what remains are Calls a
  button runs once (`docker.check_updates`, `wireguard.peer_config_download`,
  `tasks.cancel`, folder validation, the network handoff steps). A raw
  `useMutation` around `call()` would lose the shared toasts and invalidation.
- **`logging.set_debug` stays a Call** (2026-10-28): it is instant and
  idempotent, and its point is the calling bridge's own level, which a job in
  the job host could not change. Job hosts read the persisted level when they
  start.
- **Docker storage switch is one systemd restart** (2026-10-29, D1): the route
  stopped Docker and started it again as two steps of its own, so a job that
  died in between left Docker and every container down (systemd never restarts
  a deliberately stopped unit, and `docker.socket` was stopped too). A
  checkpoint would not help, because `Resume` runs only after a reboot. The
  route now writes `daemon.json` and the data-root drop-in while Docker runs
  (dockerd reads them at start) and queues `RestartUnit`; it waits for the unit
  to fail or to be active on a new `InvocationID`, for at most three minutes.
  Accepted gap: a host that dies after writing the files but before the
  restart request leaves Docker on its old settings; the job is marked
  interrupted and alerted, and applying again finishes the switch.

Phase 3 (2026-10-29 to 2026-11-08), with the cost where it was recorded. D1 is
the Docker entry above.

- **D2, balance Suspend pauses on every host stop** (user, 2026-11-05).
  `systemctl stop linuxio-jobs@0` leaves the balance paused, and a paused
  balance holds the pool's exclusive operation: remove, replace and resize are
  refused until the next mount or `btrfs balance resume`. The alternative,
  pausing only when systemd's `SystemState` is `stopping` and cancelling
  otherwise, was not taken. Cost if wrong: a stop by hand blocks other pool operations until the
  host resumes the balance.
- **D3, a replace is followed, never restarted** (user, 2026-11-05), against
  the spec's "else start it again". The request names the target by path and
  paths are not stable across reboots, so a restart could write to whichever
  blank disk now has that name. The kernel tracks a replace it resumes by
  device id. `FINISHED` is trusted only when the kernel's `time_started` is not
  before the job's own start (a power cut on small disks can let the kernel
  finish the resumed replace before the host starts, and `STATUS` can also
  report an earlier replace's `FINISHED`). Cost if wrong: a clock step back
  between the job's start and an older replace could misattribute a `FINISHED`;
  an older replace that began after this job is impossible while this job holds
  the exclusive operation.
- **D4, one resume, for the last shutdown** (user, 2026-11-05), against "once
  per boot". `ReconcileCrashed` and `markLost` kept the dead executor's boot
  id, so a job a crash or a stop by hand interrupted would have been resumed at
  some later reboot, possibly weeks on. Only `shutdown` and `reboot` records of
  an earlier boot without `cancel_requested` are resumed, and every other
  interrupted record loses `Resumable` at host start. No manual resume.
- **D5, the test VM** (user, 2026-10-29 and 2026-11-05): the agent runs the
  kernel tests itself in a disposable libvirt domain, `linuxio-e2e`, which
  touches only its own domain and volumes. `virsh destroy` is the power cut.
  This is Tier 3 of [e2e-testing.md](e2e-testing.md), started in phase 2.
- **D6, the jobs CLI goes** (user, 2026-10-29): `jobs submit` and `jobs.resume`
  existed only for testing. Task 12 removed them after the VM checks replaced
  them; phase 4 designs how timers reach the job store (section 4.1).
- **D7, a login is not capped** (user, 2026-11-10). `linuxio-auth@.service`
  declared `TasksMax=1024` and `MemoryMax=2G` and the architecture doc said
  they bounded the bridge. They did not: `pam_systemd` moves the login into a
  logind session scope (`session-<n>.scope` under `user-<uid>.slice`, the
  `_SYSTEMD_CGROUP` of every bridge line in the test VM's journal), as it does
  an SSH login. A real cap would also cover the web terminal and the
  assistant's agents, which run in the session. The two lines are removed and
  an installer fixture test keeps them out; jobs stay bounded by their host.
  Where `pam_systemd` is absent the lines did cap the session; that cap is
  gone too. Not pursued: `systemd.memory_max` and `systemd.tasks_max`, which
  `pam_systemd` reads as PAM data set by another module (untested).
- **D8, scheduled work keeps its own unit** (user, 2026-11-10), against a
  command that hands each timer firing to the root host. A script runs as a
  configured user with an arbitrary command, the snapshot, resync and Docker
  update units already work and alert on their own, and D6 removed the only
  verb that started a job from outside the browser. Each unit gets two hooks
  that write its record (section 2.14). Also the user's: a script's output is
  read live on the Schedules page, from the journal; our own programs report
  progress; every ended run keeps up to 10 MiB of output next to its record;
  scheduled runs have their own limit (the newest 50 per unit, 30 days); the
  bell offers no stop for them and opens the owning page; a failed script
  raises the same alert as a failed browser job. Dropped: log files of our own
  in place of the journal (the Logs page would lose the output, and size
  limits and clean-up would be ours to build), and showing stdout and stderr
  apart (one journal stream; the workaround lost lines).
- **Signals are masked, not retried.** Retrying an aborted replace is not an
  option (the target is destroyed), so the ioctl never sees the signal: section 5.
- **A freeze is not handled.** A suspend to RAM still aborts a scrub or replace
  on 6.19 and later.
- **Kernel refusals are errors**, for every `fsIoctl` caller.
- **Rollback is narrow.** Only superblocks with the create's own fsid are
  wiped, before the partition they sit in is deleted, only partitions the
  request created are deleted, never once the pool is in fstab, and a
  `force`-overwritten disk is not restored. udisks2 and Cockpit do no rollback
  at all; this goes further, and no further.
- **Native ioctls, not btrfs-progs output**, for every phase 3 operation.
- **11c: mount a pool a replace target holds back** (user, 2026-11-06, option
  (a)). The alternatives were a Resume hook that mounts the pool from fstab and
  documenting a manual mount. The kernel's ready check counts the devid 0
  target, so the pool never reads complete; no workaround exists upstream. The
  service mounts the pool with its fstab options, not `degraded`, unless its
  disks disagree on their superblock generation (the kernel itself would not
  refuse; this is LinuxIO's caution, as on the degraded path). A pool both
  missing a member and mid-replace stays on the degraded path. Cost if wrong:
  a power cut inside the superblock writes leaves that pool for a person, with
  the alert saying why. The bridge's mount button and the overview still count
  the target as a disk (section 3.6): a follow-up, because the kernel's refusal
  keeps the data safe.
- **No separate final review of the whole branch** (user, 2026-11-08): the VM
  suite ran once, and a fix wave got a scoped re-review.

## 7. Host-event checks

These run in the test VM ([Test VM](../development.md#test-vm)): after
`make vm-install`, `make test-vm-browser` runs
`frontend/src/test/vm/jobs-phase2.spec.ts` and `jobs-phase3.spec.ts`, with the
other specs in that folder; pass `PLAYWRIGHT_ARGS='src/test/vm/login.spec.ts
src/test/vm/jobs-phase2.spec.ts src/test/vm/jobs-phase3.spec.ts'` to run only
these three. The specs sign in as the VM's sudo user, so their jobs run in the
root job host, and start each one in the browser. The evidence is the record in
`/var/lib/linuxio/jobs/0`, read over SSH, and the host's journal. The kernel
is asked directly whether an operation still runs (a `BTRFS_IOC_SCRUB_PROGRESS`
probe, sysfs `exclusive_operation`), because `btrfs scrub status` cannot see a
scrub it did not start. Long jobs are slowed so they outlast the event: a
scrub with each disk's `scrub_speed_max` capped, other btrfs operations with
`linuxio-vm.sh throttle` (`virsh blkdeviotune`, dropped by `make vm-reset`), a
copy with the host's reads capped through `systemctl set-property`. The events
act on the domain `linuxio-e2e` alone, through `packaging/vm/linuxio-vm.sh`.
`make vm-test-btrfs-integration` runs the root kernel tests in the same VM.

| Event | How | Expect |
|---|---|---|
| stop | `systemctl stop linuxio-jobs@0.service` | record `interrupted/shutdown`; the alert `storage.btrfs_scrub_start was interrupted` in the tray. A bridge still connected restarts the host in the same boot, which clears `Resumable` |
| crash | `kill -9` the host's main pid; the browser's bridge reconnects and the socket starts a new host | record `interrupted/crash`; a later reboot does not resume it |
| orderly reboot | browser closed, `linuxio-vm.sh reboot` (`virsh reboot`) | the root host started at boot with nobody logged in; a scrub is resumed: same job `completed`, its checkpoint kept, `job resumed` in the boot's journal |
| power loss | a 2 GiB copy (not resumable) with the browser closed and a `sync` in the VM, `linuxio-vm.sh power-cut` (`virsh destroy`, then `virsh start`) | record `interrupted/reboot`; `job marked interrupted` with its id and reason `reboot` in `journalctl -b -u linuxio-jobs@0.service`; no `job resumed` |
| balance stop, then reboot | balance data raid1 to raid0 on 256 MiB chunks; sign out; `systemctl stop`; `virsh reboot` | after the stop: `interrupted/shutdown`, `{"paused":true}`, `balance paused` in sysfs; after the boot: the kernel resumes it, `job resumed`, `completed` |
| scrub power loss | scrub at 4 MiB/s per disk, `sync`, `power-cut` | `interrupted/reboot`, `job resumed`, `completed` |
| replace power loss | replace of `vdc` onto `vde` capped at 16 MiB/s, `sync`, `power-cut` | the pool mounted within 60 s of boot, `continuing dev_replace` in the kernel log, the job `completed`, the pool on `vdb` and `vde` |
| cancel, then reboot | "Stop check" on a running scrub; `virsh reboot` | the record stays `canceled`, no `job resumed`, no kernel scrub |
| crash, then reboot | `kill -9` of the host mid-scrub; `virsh reboot` | stays `interrupted/crash`, `Resumable` cleared, no `job resumed` |
| pool create rollback | a file where the mountpoint goes; create a pool on a blank disk | `failed`, `rolled back failed btrfs pool create`, no partition and no btrfs magic left |
| docker switch kill | change the Docker data root, then `kill -9` the host's main pid once the containers have stopped | Docker running, never stopped; record `interrupted/crash` with an alert |

The power-loss cases `sync` first because a job's first record write is not
fsynced (section 2.10): a power loss within the kernel's 30 s writeback may
lose the record of a job that just started. That window is not tested.
