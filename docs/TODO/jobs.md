# Jobs: mutations that outlive the login

## Status

This is the one document for the jobs work: the design, the plan for every
phase, what is done, what is left, and every item deferred by the reviews. It
replaces the jobs design spec, the phase 1 implementation plan and the local
ledger kept while phase 1 ran. Their history is in git:
`git log -- '*jobs-design.md' '*jobs-phase-1.md'`.
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
| 2 | Every mutation runs as a job: prerequisites, the ~199 routes and their callers, old task code deleted | in progress: step 1 and batch 1 of step 2 done (section 4.2) |
| 3 | btrfs resume hooks, with kill-and-resume root tests | not started |
| 4 | Schedules and system jobs | not started; one open decision |
| 5 | History view, login notice, update banner | not started |

Contents: 1. Summary · 2. Design · 3. Done · 4. Left to do · 5. Deferred
items · 6. Decision log · 7. Manual verification

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
resumes the resumable ones once per boot.

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
| Reboot and power loss | Running jobs become `interrupted`. The root host starts at boot, restarts the resumable ones (btrfs scrub, balance, replace, remove) and alerts on the rest. |
| Orderly shutdown | Resumable jobs pause and checkpoint on SIGTERM so the restart continues where they were. |
| Updates | Hosts are not part of `linuxio.target` and are never restarted by the installer. An old host finishes its jobs and exits when idle. |
| History | On disk, 30 days or 200 jobs per user. Jobs that succeed in under a second are not kept. |
| Alerts | A failed or interrupted job raises an alert (admin hosts). Success goes to history only. |
| Scheduled scripts | Stay systemd timers and services. They appear in the same list and history and use the same alert hook. |
| Jobs with no user | An admin shell submits jobs to the root host with `linuxio-bridge jobs submit`; how timers do it is open for phase 4 (section 2.14). One executor, same record, same list. |
| Logs | journald. Host jobs carry a `JOB_ID` field; unit jobs are their own unit. |
| Rejected | A system-wide root daemon running jobs (TrueNAS shape): it would run non-admins' work as root. A process per job started from the bridge: 15 ms each, which forces a fast/slow decision per route. ZFS: out-of-tree module on the user's distro. |

### 2.3 Terms

- **Bridge**: today's per-login process, as the user. Keeps reads, terminals, uploads and downloads. Relays mutations to the host.
- **Host**: `linuxio-jobs@<uid>.service`. Runs every mutation for that uid as a goroutine. Owns the in-memory job registry for that uid and writes the store.
- **Auth**: `linuxio-auth` starts an unprivileged login's host once PAM has authenticated the user, before the session opens and the bridge spawns.
- **Submit CLI**: `linuxio-bridge jobs submit`, the local client that starts a job in a host without a browser: an admin shell and tests, and timers if phase 4 keeps it. As root it starts the root host's socket itself; as a user it reaches their socket, which exists once they have logged in since boot.
- **Store**: `/var/lib/linuxio/jobs/<uid>/`, one JSON record per job, owned by the uid.
- **Job**: a record plus a handler run in a host. Started by a user through the bridge, or by the system through the submit CLI. The host is the only executor.
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

   admin shell, as root ──jobs submit──▶ linuxio-jobs@0.socket ──▶ linuxio-jobs@0.service
                          (same socket, same host, same record; timers are open, section 2.14)
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

- **Start**: by a connection to its socket, which systemd holds. `linuxio-auth` starts a user's socket when they log in (section 2.9); root processes start uid 0's with `StartUnit` on `linuxio-jobs@0.socket`, which the submit CLI does itself, and `linuxio-jobs@0.service` is enabled so the root host also starts at boot. Starting a listening socket or a running host is a no-op. `StateDirectory` and `RuntimeDirectory` create the store and progress directories owned by the uid; no custom code creates them.
- **Stay up while** connected bridges > 0 or running jobs > 0.
- **Exit** when both are zero for one minute (so a logout and login within seconds does not restart it). Exit is `sd_notify(STOPPING=1)` then process exit; systemd collects the unit.
- **Stop by systemd** (shutdown, or `systemctl stop`): SIGTERM. The host marks every running job: resumable ones get up to `TimeoutStopSec` to suspend (section 2.11), the rest are marked `interrupted` with reason `shutdown`. Then it exits.
- **Crash**: the next host for that uid, started by the next connection, finds records in `running` whose executor pid and start time are not alive and marks them `interrupted` with reason `crash`; those whose executor ran in an earlier boot get reason `reboot` (section 2.11).
- **Limits**: a host refuses a new job above 256 running jobs for its uid with 409. This is a safety valve against a runaway client, not a UX limit.

### 2.9 Who starts a host

A connection to its socket. `linuxio-jobs@<uid>.socket` listens on `/run/linuxio/jobs/<uid>.sock`, owned by the uid with mode `0600`, so only that uid and root can connect. systemd holds the socket and starts `linuxio-jobs@<uid>.service` whenever a connection finds the host down: after an idle exit, a crash, an OOM kill of the host process, or an update that replaced its binary. A connection that arrives while the host is exiting waits in the socket's queue and starts the next host, so the idle exit cannot strand a login. A host that cannot start closes the connections that were waiting, so the clients' backoff paces the next attempt instead of systemd's start limit failing the socket.

`linuxio-auth` starts an unprivileged login's socket. Once PAM has authenticated the user and the sudoers probe has decided the login is unprivileged, it runs `/usr/bin/systemctl start --no-block --quiet linuxio-jobs@<uid>.socket` as root in a child with a cleared environment, bounded by 3 s and the login's request deadline. It does so before the PAM session opens, because `pam_selinux` gives later children the user's exec context; systemctl runs as the auth worker, like the sudo probe. The bridge is spawned after the session opens. The uid is the one PAM resolved; nothing from the wire reaches the command. A failure is a warning in the journal with the user and the unit; the login proceeds and the bridge answers mutations with 503 until the next login.

A privileged bridge runs as root and calls `StartUnit` on `linuxio-jobs@0.socket` itself, as does the submit CLI run as root. A socket stays until reboot, so a user's socket exists only after that user authenticated through LinuxIO, or when root starts it.

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

Record fields: `id`, `route`, `owner {uid, username}`, `target []string`, `state`, `reason` (for `interrupted`: `shutdown`, `crash`, `reboot`), `created_at`, `started_at`, `finished_at`, `request` (the full decoded request, needed to run or resume; never returned by any API), `progress` (last envelope), `result` or `error {code, message}`, `executor {kind: host|unit, pid, pid_start, unit, invocation_id}`, `resumable`, `checkpoint` (opaque per route, section 2.11), `protocol`.

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

**Cancel.** `tasks.cancel` or an abort sets `cancel_requested` in the record *first*, then cancels the context. The handler's own cleanup runs (removing a partial copy, `BALANCE_CTL cancel`, `SCRUB_CANCEL`, `DEV_REPLACE cancel`). The record becomes `canceled` only when the handler has returned.

**Interrupt.** A SIGTERM to the host with no `cancel_requested` on a job is a shutdown. For a non-resumable job the context is cancelled and the record is written `interrupted/shutdown`. For a resumable job the host calls the route's `Suspend(ctx, record)` hook with a deadline inside `TimeoutStopSec`; the hook returns the `checkpoint` and the record is written `interrupted/shutdown`. The contract is generic: any route may declare the two hooks, and the host and the store know nothing about what they do. The first four resumable routes are the btrfs operations, because the kernel ties them to a process:

| Route | Suspend | Resume |
|---|---|---|
| `storage.btrfs_balance` | `BALANCE_CTL pause`; the kernel keeps the balance item and resumes it at the next mount by itself | attach an observer: `BALANCE_PROGRESS` until the kernel finishes |
| `storage.btrfs_scrub_start` | `SCRUB_CANCEL`; store each device's `last_physical` | restart each device's scrub from its stored offset |
| `storage.btrfs_device_replace` | nothing to do: the kernel cancels on any signal; after a crash the kernel resumes from its item | if the kernel shows the replace running, observe; else start it again with the same source and target |
| `storage.btrfs_device_remove` | nothing: cancelled by the kernel on signal; moved chunks stay moved | issue the remove again |

**Host start and boot.** Every host, before it serves, marks the records its uid's last host left `running`: `interrupted/reboot` when that host ran in an earlier boot (executors carry the boot ID; a power loss leaves records `running`, an orderly shutdown already marked them `shutdown`), `interrupted/crash` when it ran in this boot and is gone. A host touches only its own uid's store. `linuxio-jobs@0.service` is enabled, so the root host starts at boot, after `local-fs.target` and the degraded pool mounts (`linuxio-btrfs-degraded.service`). For each `interrupted` resumable record an earlier boot left (always uid 0) it runs the route's `Resume` hook on that record, and the outcome is written to the same record. That happens once per boot: a job interrupted in this boot was stopped on purpose or crashed the host, so it stays interrupted, and a job the host cannot resume is alerted and stamped with its executor so later starts leave it alone. The jobs it marks and does not resume raise one alert each. The host then exits under its normal rule once the resumed jobs end.

**Submit CLI.** `linuxio-bridge jobs submit --route <route> --request <json> [--wait]` creates a job in the caller's host exactly as a bridge would: as root it calls `StartUnit` on `linuxio-jobs@0.socket` and connects to it; as a user it connects to their socket, which exists once they have logged in to LinuxIO since boot. `--resume <id>` resumes an interrupted resumable record. `--wait` follows the job and exits with its outcome. It is the bridge binary's only verb besides `version`, kept for admin shells and tests; whether timers use it is open for phase 4 (section 2.14).

#### Fault matrix

What each event does to a job, as phase 1 implements it:

- **Page reload, tab close, connection loss**: the stream detaches and the job runs on. Lost before the `task.snapshot` frame, the client resends the same envelope once (it attaches, or gets the stored outcome); lost after it, the client reattaches with `tasks.watch`. Each is tried once, so a resend that reached the job still reattaches once; a further loss is `outcome_unknown`.
- **Resend of an `id` with another route or request**: 409.
- **Logout, session expiry, bridge exit**: the bridge closes its host connection; jobs run on. The host exits one minute after its last bridge left and its last job ended. A later login of the uid sees the jobs the host still holds through `tasks.list` and `tasks.events`.
- **Host unreachable from the bridge**: mutations fail with 503 before anything starts; `tasks.get`, `tasks.cancel` and `tasks.watch` of a host job wait up to 10 s for the host, then answer 503; the bridge reconnects with backoff from 1 s to 30 s. An open `tasks.events` stream gets, on the next connection, the end of every job it saw running.
- **Host on another protocol (an update in between)**: new mutations fail with 409 `update_pending`; reads work; the old host finishes its jobs.
- **LinuxIO update**: hosts and their sockets are outside `linuxio.target`; the installer never stops them. A host still running the old binary answers a newer bridge `update_pending` until it exits; the next connection then starts the new one.
- **`systemctl stop` or orderly shutdown**: running jobs become `interrupted/shutdown`, resumable ones after `Suspend` stored a checkpoint; the root host alerts.
- **Host crash or kill**: the next start of that uid's host marks its `running` records whose pid and start time are gone `interrupted/crash`; the root host alerts.
- **Reboot or power loss**: the root host, started at boot, marks its `running` records `interrupted/reboot`, resumes the resumable ones and alerts on the rest; a user's host marks theirs the same way when their next login connects.
- **Cancel** (`tasks.cancel`, stream abort): the job's context is cancelled and its record becomes `canceled` at once; the target stays taken until the handler returns. `cancel_requested` is not written yet (phase 3).

### 2.12 Updates

- `linuxio.target` does not pull in hosts or their sockets. The installer never stops or restarts them; it may `daemon-reload`.
- The hello frame carries a protocol number. Same number: carry on. Older host: the bridge refuses new mutations with error code `update_pending` and message "LinuxIO update waits for N running jobs"; reads work; the UI shows the count on the update banner. The old host exits when idle under its normal rule and the next login starts the new binary.
- The protocol number changes when a frame changes shape or a route moves between the bridge and the host; adding a route to the host does not change it.

### 2.13 Alerts

- `failed` and `interrupted` jobs raise an alert through `alerts.Raise` with source `jobs`, key `<id>`, the route, target and error message, and a link to the job in the UI. `canceled` and `completed` do not.
- The alert store is root-owned, so only the root host raises alerts; a user host writes the record only. The frontend shows every user "N jobs ended while you were away" from `tasks.list` at login, so non-admins still learn about failures.
- Scheduled scripts use the existing `OnFailure=` hook (`linuxio alert raise`), unchanged.

### 2.14 Scheduled scripts and system jobs

- Schedules stay as built: a timer and a oneshot service per schedule, `run_now` and `stop` as quick mutations, status from systemd.
- The job list includes running scripts by reading systemd: `linuxio-schedule-*.service` and the btrfs timer units, with the invocation ID as job ID, `ActiveState` as state and the journal as log. This is a read-only adapter in the host's `tasks.list`/`tasks.events`; scripts are the one kind of work that does not run in a host, because they are arbitrary commands under a configured user.
- **Open for phase 4: how a timer reaches the job store.** A timer can only start a unit that runs a program, so either a command hands each firing to the root host, where it runs with the same progress, record and alert as a browser job, or scheduled work keeps running in its own unit, as the btrfs snapshot, health and resync units and the Docker update timer do today, and writes a record (`executor.kind=unit`, the invocation ID) when it ends so it appears in history. The first needs a command such as `jobs submit --from <scheduled-job-id> --wait`, in the bridge or in the `linuxio` admin CLI; the second needs a small record writer. The bridge binary gains no new verb either way.

### 2.15 Security

- Nothing new is reachable from the network. The webserver still relays bytes to a bridge that is the user.
- Authorization is the uid of the connecting process, read by the kernel. The root host accepts only root; a user host accepts only its uid. `linuxio-auth` starts a host only for the uid PAM admitted.
- The record contains the request. It is `0600`, owned by the uid, and no API returns it; snapshots expose `route`, `target`, `owner`, `progress`, `result`, `error` and timestamps, as today.
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
- **Submit CLI and host start**: `jobs submit` against a host in the test process; at host start, records of an earlier boot become `interrupted/reboot` and the resumable ones are resumed, a `running` record with a dead pid of this boot becomes `interrupted/crash`, and the rest are alerted. Unit tests with the fake kernel for btrfs.
- **btrfs suspend and resume**: added to `make test-btrfs-integration` (root, run by the user): kill the host mid-scrub and check the pool is consistent; SIGTERM a balance and check it resumes after unmount and mount; scrub resumes from its stored offset; a killed replace is reported and restarted.
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
| 7 | Job client and `jobs submit`, built-in `jobs.resume` |
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

All with the installed build of `0c82d2d7` (section 7 lists the procedure):

| Case | Result |
|---|---|
| Update through the host, detached client, stop mid-job, kill mid-job, failing job, same target twice, two targets at once | pass |
| Killed or stopped host comes back on the next connection | pass; a logged-in bridge brought it back within 1 s |
| Idle exit after 60 s, socket keeps listening, one connection wakes the host | pass |
| Non-admin login: socket `teste 600`, a stopped host wakes on that user's connection, root route refused with 403 | pass |
| Only `version` and `jobs submit` accepted; launcher socket and reconcile unit gone; root host enabled | pass |
| Orderly reboot with a job running: record `interrupted/shutdown`; root host starts at boot with nobody logged in | pass |
| Power-loss reboot: record `interrupted/reboot` marked by the root host at boot | not run (unit tests only) |

## 4. Left to do

### 4.1 Open decisions

- **`jobs submit`.** Nothing in production calls it; it serves admin shells
  and the manual tests. Phase 4 needs a way for a timer to hand a run to the
  root host, or decides that scheduled work keeps running in its own unit and
  only writes a record (section 2.14). Options: drop it now and test through
  the browser; keep it until phase 4; move it into the `linuxio` admin CLI.
- **The bridge has no resource limits.** `linuxio-auth@.service` declares
  `TasksMax=1024` and `MemoryMax=2G` and the architecture doc says they bound
  the bridge, but `pam_systemd` moves the login into a logind session scope
  (`session-<n>.scope` under `user-<uid>.slice`, seen in the journal's
  `_SYSTEMD_CGROUP`), so no bridge is bounded. The installed `pam_systemd`
  supports `systemd.memory_max` and `systemd.tasks_max`; `linuxio-auth` could
  set them with `pam_putenv` before the session opens. Untested. The doc lines
  stay until this is decided.

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
  - `storage.btrfs_device_remove`: cancel issues the kernel cancel instead of
    only marking the task.
  - `storage.btrfs_pool_create`: on failure, undo the partitions it created
    and wipe the superblocks it wrote.
  - `docker.apply_daemon_storage`: write a checkpoint after stopping Docker so
    an interrupted run restarts Docker at the next host start rather than
    leaving it down.
  - The 13 routes missing `Privileged()`.
  - `/etc/default/btrfsmaintenance` and the WireGuard configs: atomic writes.
- Frontend: `useCallMutation(linuxio.<domain>.<name>, config)` becomes
  `linuxio.<domain>.<name>.useMutation(config)` (about 190 calls in 80
  files); the `useTaskStreamAction` callers and the transfer engine in
  `frontend/src/tasks/` move to `.useMutation` with `onProgress`.
- `jobs.Protocol` goes up once, because routes move from the bridge to the
  host: an older host answers a newer bridge `update_pending` and exits when
  idle instead of running a route as a `Call`.

Batches:

1. **Done, not yet committed.** The 92 writes of the accounts, alerts, config,
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
2. Docker, with its two task routes.
3. The file browser and the transfer engine in `frontend/src/tasks/`.
4. Storage, with the fixes above: an independent review and root tests.
5. The remaining task routes: `control.app_update`, `packages.update`,
   `system.install_capability`, `virt.create`, `virt.template_download`.

**Step 3: delete the old task code.** `useCallMutation`, `useTaskAction`,
`useTaskStreamAction`, `TaskRunner`, `SessionTask`, `DurableTask`,
`WithTaskLifetime`, `WithTaskIdentity`, `WithTaskProgress` and the
`TaskPolicy` admission; the guard tests of section 2.5;
`docs/api-contract.md` route counts, modes and the lifetime section.

### 4.3 Phase 3: btrfs resume

`Resumable` hooks for `storage.btrfs_scrub_start`, `storage.btrfs_balance`,
`storage.btrfs_device_replace` and `storage.btrfs_device_remove` (section
2.11), with kill-and-resume steps in `make test-btrfs-integration` (root, run
by the user). Data-safety work: upstream prior art and an independent review
first. Cancel writes `cancel_requested` before it cancels, and the record turns
`canceled` only when the handler returns (section 2.11), so a cancelled job is
never resumed.

### 4.4 Phase 4: schedules and system jobs

Decide how timers reach the job store (section 2.14 and 4.1); the read-only
adapter that lists `linuxio-schedule-*.service` and the btrfs timer units in
`tasks.list`; btrfs timer units writing records.

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

- [ ] Redundant derived count in `TestAllTaskRoutesUseTaskRunner` (goes away in phase 2, step 3).
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
- [ ] The CLI accepts a malformed `--resume` id, and `--wait` beside `--resume` (depends on the `jobs submit` decision).
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
- [ ] *Gap.* Cancel marks the record canceled at once without `cancel_requested` (moved to phase 3, where resume needs it). The daily prune was added in phase 2, step 1.
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
- Accepted: records written before boot IDs existed are marked `crash` instead of `reboot` (only unreleased builds wrote them; no route is resumable yet).
- Accepted: the explicit `After=` on the socket is redundant with systemd's implicit ordering.

### Other open items

- [ ] The power-loss reboot path (`interrupted/reboot` marked at boot) has only unit-test coverage.
- [ ] The bridge resource limits (section 4.1).

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

## 7. Manual verification

Run on an installed build, as root, after logging in once through the browser
as an admin. The commands use a throwaway container whose update takes about
15 s: `sleep` as PID 1 ignores SIGTERM, so Docker waits the stop timeout.

```bash
# A container that always has an update: local alpine:3.21 is really 3.20.
docker pull -q alpine:3.20 && docker tag alpine:3.20 alpine:3.21
docker rm -f jobs-smoke; docker run -d --name jobs-smoke --stop-timeout 15 alpine:3.21 sleep infinity
# Run one update and follow it; without --wait the command returns at once.
linuxio-bridge jobs submit --route docker.update_container --request '{"containerId":"jobs-smoke"}' --wait
# Read a record (the directory is root-only, so let root expand the glob).
sh -c 'ls -t /var/lib/linuxio/jobs/0/*.json | head -1 | xargs grep -E "\"(state|reason)\""'
```

| Case | How | Expect |
|---|---|---|
| complete | submit with `--wait` | exit 0, `"updated":true`, record `completed` |
| detach | submit without `--wait`, wait | record `completed` |
| stop | submit, after 3 s `systemctl stop linuxio-jobs@0.service` | `interrupted/shutdown`, an alert |
| crash | submit, after 3 s `kill -9` the host's main pid, then submit again | first record `interrupted/crash`, an alert |
| fail | submit for a container that does not exist | exit 1, record `failed`, an alert |
| same | two submits for the same container | the second refused, "already running on this target" |
| twice | submits for two containers | both `completed`; the second waited for the update lock |
| wake | kill or stop the host, then connect to `/run/linuxio/jobs/0.sock` | the host is active again |
| verbs | `linuxio-bridge jobs host`, `jobs reconcile`, `jobs launch`, and no arguments | exit 2 each |
| idle | log out of the browser, wait 70 s | host inactive, socket active; one connection wakes it |
| non-admin | log in as a user without sudo, stop their host, submit as them | socket owned by them, mode 600; host woken; root route refused with 403 |
| orderly reboot | submit, then `systemctl reboot` | `interrupted/shutdown`; the root host starts at boot |
| power loss | submit, then `systemctl reboot --force --force` (test machines only) | `interrupted/reboot` and `job marked interrupted` in `journalctl -b -u linuxio-jobs@0.service` |

The stop, crash and power-loss cases leave the container update half done; the
next update of that container recovers it.
