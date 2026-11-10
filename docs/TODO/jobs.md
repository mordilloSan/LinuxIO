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
| 4 | Scheduled runs in the job list and the store | designed (section 2.14, D8), not built; [plan](../superpowers/plans/2026-11-10-jobs-phase4-scheduled-runs.md) |
| 5 | History view, login notice, update banner | not started |

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
| Alerts | A failed or interrupted job raises an alert (admin hosts). Success goes to history only. |
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

Record fields: `id`, `route`, `owner {uid, username}`, `target []string`, `state`, `reason` (for `interrupted`: `shutdown`, `crash`, `reboot`), `created_at`, `started_at`, `finished_at`, `request` (the full decoded request, needed to run or resume; never returned by any API), `metadata` (the public projection shown with the job; scheduled runs write it, phase 4), `progress` (last envelope), `result` or `error {code, message}`, `executor {kind: host|unit, pid, pid_start, unit, invocation_id}`, `resumable`, `checkpoint` (opaque per route, section 2.11), `protocol`.

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
- **Host on another protocol (an update in between)**: new mutations fail with 409 `update_pending`; reads work; the old host finishes its jobs.
- **LinuxIO update**: hosts and their sockets are outside `linuxio.target`; the installer never stops them. A host still running the old binary answers a newer bridge `update_pending` until it exits; the next connection then starts the new one.
- **`systemctl stop` or orderly shutdown**: running jobs become `interrupted/shutdown`, resumable ones after `Suspend` stored a checkpoint and before their context is cancelled; the root host alerts. A job with `cancel_requested` is left to end `canceled`.
- **Host crash or kill**: the next start of that uid's host marks its `running` records whose pid and start time are gone `interrupted/crash`; the root host alerts.
- **Reboot or power loss**: the root host, started at boot, marks its `running` records `interrupted/reboot`, resumes the resumable ones (rule above) and alerts on the rest; a user's host marks theirs the same way when their next login connects.
- **Cancel** (`tasks.cancel`, stream abort): `cancel_requested` is written to the record, then the job's context is cancelled; the record becomes `canceled` only when the handler returns, whatever error it returns after the cancel, and the target stays taken until then. A snapshot stays `running` meanwhile and carries `cancel_requested`; the tray row and the storage controls read "Cancelling…" until the job ends (section 3.7).

### 2.12 Updates

- `linuxio.target` does not pull in hosts or their sockets. The installer never stops or restarts them; it may `daemon-reload`.
- The hello frame carries a protocol number. Same number: carry on. Older host: the bridge refuses new mutations with error code `update_pending` and message "LinuxIO update waits for N running jobs"; reads work; the UI shows the count on the update banner. The old host exits when idle under its normal rule and the next login starts the new binary.
- The protocol number changes when a frame changes shape or a route moves between the bridge and the host; adding a route to the host does not change it.

### 2.13 Alerts

- `failed` and `interrupted` jobs raise an alert through `alerts.Raise` with source `jobs`, key `<id>`, the route, target and error message, and a link to the job in the UI. `canceled` and `completed` do not.
- The alert store is root-owned, so only the root host raises alerts; a user host writes the record only. The frontend shows every user "N jobs ended while you were away" from `tasks.list` at login, so non-admins still learn about failures.
- Scheduled scripts use the existing `OnFailure=` hook (`linuxio alert raise`), unchanged.

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

- `begin` creates the `running` record: `id` is `$INVOCATION_ID` written as a UUID, `route` and label come from the unit name, owner is root, `target` is `[unit]`, `metadata` is `{"label", "identity": [<schedule id or pool uuid>]}`, executor is `{kind: unit, unit, invocation_id, boot_id}`.
- `end` reads `$SERVICE_RESULT`, `$EXIT_CODE` and `$EXIT_STATUS`: `success` is `completed`; otherwise a record with `cancel_requested` is `canceled`, a system that is shutting down gives `interrupted/shutdown`, and anything else is `failed` with a message such as "exited with status 3", "killed by signal KILL" or "timed out". It then saves the output, raises the alert and prunes. A run whose `begin` wrote nothing still gets its record from `end`.

**Store.** `/var/lib/linuxio/jobs/scheduled` with progress under `/run/linuxio/jobs/scheduled`, root only. It is separate from the per-uid stores so scheduled runs have their own limit: each unit keeps its newest 50 ended runs and nothing older than 30 days. Every run is kept, also one shorter than a second.

**Output.** `end` copies the run's journal lines (`_SYSTEMD_INVOCATION_ID`, message text only) to `<id>.log` next to the record, after `journalctl --sync`. At most 10 MiB: a longer output keeps its first and last 5 MiB with one line between saying how many bytes were left out. The file is removed with the record. Stdout and stderr are one stream and cannot be told apart (checked on systemd 259: same `_STREAM_ID`, same priority; sending stderr through a second journal connection lost lines).

**Progress.** The btrfs timer program and the Docker update runner write the store's progress file of their own run, found through `$INVOCATION_ID`. Scripts report none; their feedback is their output.

**Job list, root host only.** `tasks.list` adds the running scheduled runs, `tasks.events` polls the store every 2 s while a client listens and sends snapshot, progress and terminal events, `tasks.get` answers for a scheduled run, `tasks.cancel` and `tasks.watch` of a running one answer 409. A `running` record counts only while systemd agrees: same boot, the unit's current invocation is the record's, and the unit is `activating`, `active`, `reloading` or `deactivating`. Otherwise the host marks it `interrupted` (`reboot` for an earlier boot, `crash` in this one). Every admin sees scheduled runs; other users do not.

**Stopping.** The bell shows no stop for a scheduled run; its row opens the owning page. `schedules.stop` and a scrub cancel that hits a timer's resync set `cancel_requested` on the record first, so the run ends `canceled`.

**Alerts.** One alert per failure. A script that fails or is interrupted raises the jobs alert (source `jobs`, key the record ID, title "<label> failed" or "<label> was interrupted"), as a failed browser job does. Snapshots, replication, resync and Docker updates already raise their own alert and get no second one.

**Frontend.** The bell lists each running scheduled run with its label and progress. The Schedules page shows the output of the selected schedule's current or latest run, live, from the journal. Listing old runs and reading their saved output belongs to the history view (phase 5).

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
- A history view lists `tasks.list` for the user: route, target, owner (admins see all owners in the root host), state, duration, error, with a link to the journal filter. Small: one route, one table.
- At login the frontend fetches `tasks.list` once and shows the "ended while you were away" notice when any job finished since the last seen timestamp in UI preferences.
- The update banner shows "waits for N jobs" when a mutation returns `update_pending`.

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

### 4.4 Phase 4: schedules and system jobs

Section 2.14 is the design. The tasks are in the
[phase 4 plan](../superpowers/plans/2026-11-10-jobs-phase4-scheduled-runs.md):
the scheduled store and its limit, the two unit hooks and the output copy,
progress from the btrfs timer program and the Docker update runner, the root
host's listing and liveness check, the bell rows and the live output on the
Schedules page.

### 4.5 Phase 5: history and notices

The history view, the "ended while you were away" notice at login, the
"waits for N jobs" update banner (section 2.16).

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
