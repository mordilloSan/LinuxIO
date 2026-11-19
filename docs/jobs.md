# Jobs

A change to the host that a user starts from the UI runs as a **job** in a
**job host**: one long-lived process per uid, `linuxio-jobs@<uid>.service`,
which systemd starts when a connection reaches its socket. The per-login
bridge relays each mutation stream to it. A job therefore runs to its end
whether or not the user stays logged in, the tab stays open, the bridge stays
alive or LinuxIO updates itself, and the user finds it again at the next
login with its progress, outcome and, for scheduled runs, its output. Each
job is a JSON record on disk.

The same records hold the runs that systemd timers start (scheduled scripts,
btrfs snapshots, pool resyncs, Docker updates and the distribution's system
updates), so the bell, the history page and the alerts treat both alike.

A route author never decides whether a change is quick or slow: there is one
way to write a mutation and one way to call it. Non-goals: resuming
non-storage work after a reboot (it is reported as interrupted), moving
uploads and downloads out of the browser connection, and changing how reads
or authentication work.

Other documents own parts of this system, and this one links to them rather
than repeating them:

| Doc | What it holds |
|---|---|
| [API Contract](./api-contract.md#tasks-and-jobs) | Route modes, binding options, the stream frames, every `tasks.*` route with its requests, answers and errors |
| [Process & Systemd Architecture](./process-systemd-architecture.md#login-and-jobs) | The job host units, boot and login flow, the update wait and the needrestart override, the scheduled-run hooks |
| [Alerts](./alerts.md) | Who raises which alert at which level, the notification settings, delivery, the notice at sign-in |
| [Btrfs](./btrfs.md#shutdown-and-resume) | What the four resumable storage routes do on cancel, stop and resume |
| [Frontend Task State](./frontend-task-state-unification.md) | The tray's task cache and the upload and download engines |
| [Development](./development.md#test-vm) | The test VM the host-event checks run in |

Open work is in the [backlog](./TODO/backlog.md).

## Terms

- **Bridge**: the per-login process, running as the user (root for a
  sudo-authorized login). It serves reads, channels, terminals, uploads and
  downloads, and relays every other mutation to the job host.
- **Job host**: `linuxio-jobs@<uid>.service`, the `linuxio-bridge` binary run
  by systemd with no arguments. It runs each job of its uid as a goroutine,
  holds the in-memory task registry and writes the store. The **root host**
  is uid 0's, which every admin login shares; a **user host** is any other
  uid's.
- **Job**: a record plus a handler running in a job host, started through a
  bridge. The job host is the only executor of a mutation.
- **Scheduled run**: one run of a timer-started unit. It is not a job (no
  route starts it, no job host runs it) but writes the same kind of record.
- **Store**: a directory of records. Each uid has one, and scheduled runs have
  their own.
- **Target**: what a job works on (a pool UUID, a device, a container, a
  compose project, a set of paths), named by the route's request.

## Architecture

```text
browser ──WebSocket── linuxio-webserver ──yamux── bridge (per login, as the user)
                                                    │ Calls, channels, tasks.data,
                                                    │ the three browser-bound mutations
                                                    │
                                     every other mutation stream, relayed unchanged
                                                    ▼
linuxio-auth (root, after PAM) ── systemctl start ── linuxio-jobs@<uid>.socket
                                                    │ a connection starts the host
                                                    ▼
                                    linuxio-jobs@<uid>.service, as <uid>
                                      ├─ task registry (memory)
                                      ├─ handlers (the bridge's own code)
                                      ├─ records   /var/lib/linuxio/jobs/<uid>/
                                      └─ progress  /run/linuxio/jobs/<uid>/

timer ── linuxio-schedule-<id>.service and the other timer units
           ExecStartPre= / ExecStopPost= hooks ── /var/lib/linuxio/jobs/scheduled/
                                                  (read by the root host)
```

The webserver, `linuxio-auth` and the bridge keep their roles. Nothing new
listens on the network and no new socket is reachable by another user: a job
host's socket is `0600` and owned by its uid. The root host runs admin jobs as
root, as the root bridge runs admin calls; `linuxio-auth` gains one bounded
`systemctl start`.

Why this shape and not another:

- **Not a system-wide root daemon** running every user's jobs: it would run a
  non-admin's work as root.
- **Not a process per job** started from the bridge: a start was measured at
  about 15 ms, which would force a fast/slow decision per route.
- **Not a bridge that lingers after logout** until its jobs end: jobs would
  share the bridge's crashes and die with every update, each lingering bridge
  would hold one of `linuxio-auth.socket`'s connections (a local denial of
  service by any user), the next login would need the store as a polling bus,
  and timers and the boot resume would still need another executor.

## Writing a route that runs as a job

The binding API is in [API Contract](./api-contract.md#route-modes-and-kinds).
The rules a route author follows:

- **Every write is a `Mutation`** (`apischema.Mutation`, `MutationVoid`, or
  `MutationTask` for a runner that needs its `*bridgeipc.Task`). A `Call` is
  bounded request/response work. The API Contract's summary lists the few
  writes that stay Calls and why: `logging.set_debug` changes the calling
  bridge's own log level, which a job in the host could not, and the network
  bridge-handoff routes keep a durable record of their own.
- **No deadline.** A Mutation has no client timeout and no server-side
  default deadline. A deadline per route would bring back the fast/slow
  decision and turn a slow change into a failure part-way; a stuck job stays
  in the tray, where it can be cancelled. The handler honours its context,
  which ends only on a cancel, a stream abort or the host stopping.
- **Progress** goes through `bridgeipc.ReportProgress(ctx, ...)`, which does
  nothing outside a job, so a handler has one signature. Declare the detail
  type once with `apischema.Progress[T]()`.
- **Root work declares `apischema.Privileged()`**, as every route does
  ([Privilege Pattern](./privilege_pattern.md)). The job host checks it again
  with its own session: only the root host's sessions are privileged.
- **Name the target.** A request type that implements `Target() []string`
  (`apischema.Targeted`), or a route with a `WithTaskMetadata` identity, makes
  a second job of the same route on the same target refuse with `409` while
  the first runs. A request type without one is never refused as a
  duplicate; quick mutations mostly have none, since a refused double click
  needs UI handling of its own.
- **`apischema.Exclusive()`** runs one job of the route at a time, whatever
  its target, for work that owns a whole system resource. Five routes declare
  it (`control.app_update`, `docker.apply_daemon_storage`, `packages.update`,
  `system.install_capability`, `filebrowser.index`): two overlapping runs
  would race on the binaries, Docker's data directory, the package manager or
  the index, and their targets could not express that.
- **Tag secrets.** A request field tagged `secret:"true"` is left out of the
  stored request. `TestMutationRequestsKeepTheirSecretsOutOfTheRecord` fails
  for a field named like a password or key without the tag.
- **`apischema.BrowserBound()`** keeps a Mutation in the bridge because its
  bytes come from or go to the browser. Only `filebrowser.upload`,
  `filebrowser.upload_batch` and `filebrowser.archive` declare it; logout
  cancels their jobs.
- **`.Resumable(apischema.ResumeHooks{Suspend, Resume, At})`** lets a job
  continue after a shutdown or a power loss, or after its host died. The
  binding panics unless the route is `Privileged()` (resumption runs in the
  root host) and has no secret fields (the resumed handler needs the whole
  request). `At` says at which starts of the root host it resumes: after a
  reboot when it is zero, and with `bridgeipc.ResumeAfterCrash` or
  `ResumeAfterStop` (both: `ResumeInBoot`) at the next start in the same boot
  after the host crashed or was stopped by hand, for work that runs on outside
  the host and that `Resume` only follows. A handler that must be found again after a crash, which runs
  no `Suspend`, saves where it stands with `bridgeipc.SaveCheckpoint(ctx, v)`.
  The four btrfs operations, `packages.update` and `storage.run_smart_test`
  declare it ([Resume hooks](#resume-hooks)); replication is not resumable,
  since an interrupted run is reported and the timer catches up.
- **Moving a route between the bridge and the host raises `jobs.Protocol`**
  (`backend/common/jobs/record.go`), so a host of another build refuses the
  bridge instead of serving the route the other way ([Updates](#updates)).
  Adding a route to the host does not.

On the frontend a component follows a Mutation with
`linuxio.<domain>.<name>.useMutation(config)`, whose config adds `onStart`,
`onProgress`, `signal` and `closeOnAbort` to the shared action config and whose
result has `watch(taskId, variables)` to adopt a running job. The tray and
uploads start jobs with `endpoint.start`, which resolves with the first
snapshot and detaches. Query invalidation on success stays in
`frontend/src/api/operation-query-invalidations.ts`.

## The mutation stream

One stream carries the whole job ([frames](./api-contract.md#the-mutation-stream)).
The client chooses the job's `id`, a UUID, and sends it in the envelope.

- The server answers at once with a `task.snapshot` frame (the job exists and
  the `id` is claimed), then progress frames, then one result or error frame.
- Closing the stream detaches and the job runs on; an abort cancels it.
- A connection lost before the snapshot makes the client resend the same
  envelope once; one lost after it makes the client reattach once with
  `tasks.watch`. A further loss is `outcome_unknown`. A resend of a known `id`
  attaches to the running job or is answered from its record, and is never
  run again; the same `id` with another route or request is a `409`.
- Any tab or login of the same uid can find the job again through
  `tasks.events` and `tasks.list` and follow or cancel it.

## Bridge and job host

At startup the bridge connects to the host of the uid it runs as, over
`/run/linuxio/jobs/<uid>.sock` (`backend/bridge/cmd/jobs_relay.go`,
`backend/bridge/internal/jobsclient`). A privileged bridge runs as root, so
every admin login uses the root host. The dial retries for 5 s while the
socket comes up; a failed or lost connection is retried with backoff from 1 s
to 30 s. Without a connection, new mutations fail with `503` before anything
starts, and `tasks.get`, `tasks.watch`, `tasks.cancel`, `tasks.output` and
`tasks.history` first wait up to 10 s for one.

The first stream of a connection is `jobs.hello`: the protocol number and the
session facts (username, uid, gid, privileged, `session_ref`). The host has
already read the peer's uid with `SO_PEERCRED` and closed the connection
unless it is the host's own uid; a hello of another protocol is refused with
`426`. The hello's facts name the job's owner for listing, history and
alerts; they are not authorization. On the root host, whose only peers are
root, the hello's user also decides who the handlers act for: the host builds
one router per connection with that user's identity and config store, as
their privileged bridge would, so a Compose action resolves that admin's
folders and a setting lands in that admin's file. Same-target and exclusive
admission are counted in the task service that those routers share, so two
admins' jobs refuse each other.

Each mutation opens one host stream and the bridge copies frames both ways
unchanged; a close or an abort passes through. The bridge forwards
`tasks.get`, `tasks.cancel` and `tasks.watch` of a job ID it does not hold,
appends the host's jobs to `tasks.list`, and merges the host's `tasks.events`
into its own. When it reconnects it reopens that stream with the IDs of the
jobs it saw running, so a job that ended while no host was connected is seen
ending. Session end cancels nothing in the host.

## Job hosts

The units are described in
[Process & Systemd Architecture](./process-systemd-architecture.md#systemd-units);
this section says why they are as they are.

### Who starts a host

A connection to its socket. `linuxio-jobs@<uid>.socket` is held by systemd,
which starts the host whenever a connection finds it down: after an idle exit,
a crash, an OOM kill of the host process or an update that replaced its
binary. A connection that arrives while the host is exiting waits in the
socket's queue and starts the next host. Socket activation was chosen over
`Restart=on-failure` plus a login check because only a component that outlives
the host can start it at the moment a bridge connects, and systemd already is
one.

- **A user's socket** is started by `linuxio-auth`, once PAM has admitted an
  unprivileged login and before the PAM session opens:
  `systemctl start --no-block --quiet linuxio-jobs@<uid>.socket`, bounded by
  3 s and the login's request deadline (`backend/auth/linuxio-auth.c`). The
  uid is the one PAM resolved; nothing from the wire reaches the command. It
  runs before the session opens because `pam_selinux` gives later children the
  user's exec context. A failure is a warning in the journal and the login
  proceeds; that bridge then answers mutations with `503`. A user cannot start
  a system unit, and LinuxIO has one authenticated door, so this is the only
  way a user's host comes to exist: there is no world-connectable launcher.
- **The root socket** is started by a privileged bridge itself (`StartUnit`),
  and `linuxio-jobs@0.service` is enabled, so the root host also starts at boot
  to mark, alert and resume the jobs a reboot interrupted.
- A socket stays until reboot. Starting a listening socket or a running host
  is a no-op, so repeated logins cannot multiply hosts.
- As the host, the bridge binary takes no arguments: it recognises the socket
  systemd passes it by its name, `linuxio-jobs`, and refuses to run without it.
  The binary has no verb besides `version`: nothing outside the browser starts
  a job, and there is no manual resume.

### Lifecycle

- **Stays up** while a bridge is connected or a job runs.
- **Exits** 60 s after both reach zero, so a logout and a login within seconds
  do not restart it. Deciding to exit closes it to new bridges under the same
  lock, so none can join and start a job meanwhile.
- **Stops** on SIGTERM (`systemctl stop`, shutdown): it interrupts the running
  jobs ([Shutdown](#shutdown-and-interrupt)), lets the open job streams write
  their terminal frame for up to 5 s, then exits. The whole shutdown is
  bounded at 100 s, inside the unit's `TimeoutStopSec=2min`.
- **Cannot start**: it closes the connections systemd queued for it, so the
  clients' backoff paces the next attempt instead of systemd's start limit
  failing the socket for the rest of the boot.
- **Logs** at the level `logging.set_debug` persisted, read at start; a
  running host keeps that level until it idles out.

### Limits

- 256 running jobs per host; above that a new job is refused with `409`
  `too many running jobs`. This is a valve against a runaway client, not a
  user-facing limit; otherwise there is no queue and no per-user, per-route or
  runtime cap.
- `TasksMax=8192`, room for 256 jobs each waiting on a subprocess tree: below
  it the Go runtime aborts the host, with every job in it, when it cannot
  create a thread. `MemoryMax=2G` with `OOMPolicy=continue`, so an OOM kill of
  one job's subprocess fails that job instead of stopping the host.
- Hosts and their sockets are not `PartOf=linuxio.target`, so an update or
  `linuxio restart --full` never stops a running job.

## Store and records

`backend/common/jobs` implements the store. systemd creates each uid's
directories (`StateDirectory=`, `RuntimeDirectory=`, both `0700` and owned by
the uid) below `/var/lib/linuxio/jobs` and `/run/linuxio/jobs`, which tmpfiles
creates `0755 root:root` (and the store, whatever the umask, when they are
missing), so every host's own directory stays reachable.

| Path | Holds |
|---|---|
| `/var/lib/linuxio/jobs/<uid>/<id>.json` | A job's record, `0600` |
| `/var/lib/linuxio/jobs/<uid>/.lock` | The writers' lock; readers take none, since records are replaced by rename |
| `/run/linuxio/jobs/<uid>/<id>.progress` | A job's live progress, memory-backed, written at most twice a second |
| `/run/linuxio/jobs/<uid>.sock` | The host's socket, held by systemd |
| `/var/lib/linuxio/jobs/scheduled/<id>.json`, `<id>.log` | A scheduled run's record and saved output, root only ([Scheduled runs](#scheduled-runs)) |
| `/run/linuxio/jobs/scheduled/<id>.progress` | A scheduled run's progress |

Record fields (`backend/common/jobs/record.go`):

| Field | Meaning |
|---|---|
| `id`, `route` | The job ID and the route that runs it (`scheduled.*` for a scheduled run) |
| `owner` | `uid`, `gid`, `username` of the user who started it; on the root host the admin, not root |
| `target` | What the job works on, stored once: every snapshot of the record shows it as `metadata.identity` |
| `state`, `reason` | `running`, `completed`, `failed`, `canceled` or `interrupted`; an interruption's reason is `shutdown`, `crash` or `reboot`. There is no `queued` |
| `created_at`, `started_at`, `finished_at` | Times, UTC |
| `request` | The decoded request without its secret fields, kept to compare a resend and to resume. No API returns it |
| `metadata` | The public projection shown with the job: label, action, paths and the like |
| `progress`, `result`, `error` | The last progress (only the network handoff keeps one here), the result, or `{code, message}` |
| `executor` | `kind` `host` with `pid`, `pid_start` and `boot_id`, or `unit` with `unit`, `invocation_id` and `boot_id` |
| `resumable`, `checkpoint` | Whether the route can resume it, and the opaque state its `Suspend` hook returned or the job saved while it ran (`bridgeipc.SaveCheckpoint`; dropped for a job whose record could not be created) |
| `resumed` | A host resumed the job at least once |
| `cancel_requested` | Set before a cancel reaches the handler |
| `protocol` | The `jobs.Protocol` of the build that wrote it |

Unknown members are ignored when a record is read, so an older binary keeps a
record that a newer one wrote with members it does not know.

**Write policy.** A record is written when the job starts and on each change,
by rename. It is fsynced only when a job that ran longer than one second
reaches a terminal state, or when its checkpoint (written by `Suspend` at a
shutdown, or by the job itself) or its cancel request changes: a record write was measured at about 1.4 ms with an fsync on NVMe
against 35 µs without, and a spinning root disk is slower still. Progress goes to `/run` and never to the record.

**Quick jobs leave nothing.** A job that completes in under one second is
deleted from the store as it completes; one that fails, is cancelled or is
interrupted is kept. Such jobs never appear in the list or the history.

**Size.** A record is at most 1 MiB. One that would be larger sheds, in this
order, its result (which attached clients received whole), its target, its
request and last its metadata, so a batch of thousands of paths keeps its
record. Progress is at most 64 KiB.

**Retention.** Terminal records are kept 30 days and at most 200 per store; a
host prunes at start and every 24 hours. The root host's store holds every
admin's jobs, so the 200 are shared. Prune also removes a record that cannot
be read or validated, such as a torn write. Running records are never pruned.

## Job lifecycle

### Admission

The host first looks the `id` up: known with the same route and request, it
attaches to the job or answers from its record; known with another route or
request, it is a `409`; unknown, it is a new job. A new job is refused with
`503` while the host stops and with `409` beyond the 256th; its request is
then decoded, and admission refuses with `409` a second job of the same route
on the same target (`<route> is already running on this target`) and a second
job of an `Exclusive()` route. A job refused this way leaves no record.

### Run, finish and cancel

The handler runs in a goroutine with a context that ends only on
`tasks.cancel`, a stream abort or the host stopping. Progress goes to every
attached stream and, coalesced, to the progress file. At the end the record is
written `completed` or `failed` (or deleted, for a quick success) and the
result or error frame goes to the attached streams. The host keeps a terminal
job in memory for 30 minutes, then answers for it from the record.

A cancel writes `cancel_requested` to the record first and only then cancels
the context, so neither a shutdown that follows nor the next boot resumes it.
The handler's own cleanup runs (removing a partial copy, the kernel's balance,
scrub, replace or removal cancel), and the record turns `canceled` only when
the handler returns, whatever error it returns after the cancel. Until then
the target stays taken and the snapshot reads `running` with
`cancel_requested`, which `tasks.events` announces, so the tray and the
balance's **Cancel** read "Cancelling…" and the scrub's **Stop check** reads
"Stopping…", each withholding a second cancel.

### Shutdown and interrupt

On SIGTERM the host interrupts each running job, in parallel:

1. A job whose record has `cancel_requested` is left to its handler and ends
   `canceled`.
2. A job of a route with a `Suspend` hook is flagged interrupted first
   (`Task.BeginInterrupt`), so a handler the hook stops ends interrupted and
   not failed, then its `Suspend` hook runs, for up to 90 s, acting for the
   record's owner.
3. The record is written `interrupted` with reason `shutdown` and the
   checkpoint, and only then is the job's context cancelled, so the cleanup
   the cancel starts cannot turn the record `canceled`.
4. A job admitted as the host began to stop is interrupted the same way.

A client attached to an interrupted job reads `job was interrupted` (`409`).

### Host start and boot

Every host, before it serves, marks the records its uid's last host left
`running`: `interrupted/reboot` when that host ran in an earlier boot (the
executor carries the boot ID), `interrupted/crash` when it ran in this boot and
its pid and start time are gone. An orderly shutdown has already marked its
records `shutdown`; a power loss leaves them `running`. A record left `running`
with no task in the host to finish it, such as one whose finish could not be
written, is marked `interrupted/crash` when a resend or a `tasks.*` request
asks for it.

The root host then resumes, once, each record that is `interrupted`, still
`resumable` and without `cancel_requested`, when its route resumes at this
start:

- **After a reboot** (`bridgeipc.ResumeAfterReboot`, the default: the four
  btrfs operations and `storage.run_smart_test`): reason `shutdown` or
  `reboot`, and an executor that ran in an earlier boot.
- **After a crash in this boot** (`bridgeipc.ResumeAfterCrash`:
  `packages.update` and `storage.run_smart_test`): reason `crash` and an
  executor of this boot, when the run that crashed was not itself a resume
  (`resumed`): a resume that crashes its host would otherwise crash every
  start.
- **After a stop by hand in this boot** (`bridgeipc.ResumeAfterStop`:
  `storage.run_smart_test`): reason `shutdown` and an executor of this boot.
  `packages.update` does not take it: an admin may stop the host to halt an
  update.

A route that resumes in the boot does so for work that runs on outside the job
host, so the next host picks the job up from where that work stands instead of
reporting it interrupted. The next start is the one the bridge's reconnect
causes, within its 30 s backoff while an admin is signed in, or the next admin
login.

Every other interrupted record (a crash or a stop by hand of a route that does
not resume after it, a cancel that overlapped a stop) loses `resumable` at that
start, and a job a request found with no task loses it at once. So a job is
resumed only by the start right after the interruption, never weeks later by
some other reboot, and there is no manual resume. The same-boot pick-ups start
before the host accepts a connection or tells systemd it is ready, so a bridge
that reattaches to a job its crashed host left finds it running again; the
resumes after a reboot start once it serves, as before pick-ups existed,
because building a job owner's handlers (the admin's home folder, a user
lookup) can hang at boot and must not keep the host from serving. A host told
to stop while it starts leaves the jobs it has not resumed yet as they are,
for its next start. The resumed job keeps its ID, record, checkpoint and start
time and runs as a new task in this host. A job the host cannot resume raises
an alert and is stamped with this host's executor, so later starts in this
boot leave it alone; one of a route that resumes in the boot also loses
`resumable`, or each later start would try it again. A `Resume` hook whose source of
truth cannot tell what became of the work returns `bridgeipc.ErrNotResumed`
with the reason: the job stays `interrupted` with its reason, the reason in
its error, loses `resumable`, and raises the alert its interruption would
have raised. A user's host marks its records the same way when the next login
connects, and never resumes: resumable routes are privileged.

`linuxio-jobs@.service` is ordered after `local-fs.target` and
`linuxio-btrfs-degraded.service`, so pools are mounted, degraded if they must
be, before the root host resumes anything.

### Resume hooks

Six routes are resumable. Five resume after a reboot: the four btrfs
operations, `storage.btrfs_balance`, `storage.btrfs_scrub_start`,
`storage.btrfs_device_replace` and `storage.btrfs_device_remove`, and
`storage.run_smart_test` (below). What each btrfs hook does is in
[Btrfs](./btrfs.md#shutdown-and-resume). The hooks differ because the kernel
treats each operation differently:

- A fatal signal, SIGKILL included, cancels a balance in the kernel and deletes
  its item, so only a pause keeps it; the kernel continues a paused balance at
  the next read-write mount. The hook pauses on every host stop, a stop by hand
  included, rather than only when the system is stopping: a paused balance
  then holds the pool's exclusive operation until the pool is mounted again
  or the balance is resumed by hand.
- A scrub has no kernel state, so the checkpoint is each device's offset.
  The scrub also saves each device's offset and counters in its own record
  every minute under the job's ID, and `Resume` prefers that record, so a
  scrub a power loss cut continues too.
- The kernel tracks a replace in an item and continues it at mount by device
  ID, so `Resume` follows it and never starts one. Device paths are not stable
  across reboots, so a restart could write to whichever blank disk now has the
  target's name. A `FINISHED` replace counts only when the kernel's
  `time_started` is not before the job's own start.
- The kernel never continues a removal, so `Resume` issues it again.

`packages.update` resumes in the same boot, after a crash of its host only
(`backend/bridge/handlers/packages/package_update_operation.go`). PackageKit
runs a transaction to its end whether or not its client is still there, and
keeps a history of the transactions it ran, across its own restarts:

- The job updates each package in a transaction of its own. Before it asks
  PackageKit to update a package it saves, fsynced, a checkpoint with that
  package's index, the transaction's object path (PackageKit's transaction
  ID, unique across its restarts), and how many packages before it updated and
  which failed with what. Its `Suspend` keeps that checkpoint: the context
  cancel that follows stops following the transaction, which runs on.
- `Resume` watches the transaction, then asks PackageKit whether it still runs
  it (`GetTransactionList`). If it does, and its role is `update-packages`,
  the job follows it to its end with the same progress. If not, or if
  PackageKit drops it before its role is read (it keeps an ended transaction
  a few seconds), it reads the newest 200 entries of PackageKit's history
  (`GetOldTransactions`): one with
  the transaction's ID and role that succeeded counts as an updated package,
  one that failed as a failed package. Then it updates the packages it had not
  reached, and the job ends as a run that was never interrupted would.
- It never asks PackageKit to update a package again. A checkpoint missing
  (the host died before the first transaction), a transaction PackageKit
  neither runs nor recorded, one of another role, or one recorded as started
  and never ended (no duration: PackageKit itself stopped) is
  `bridgeipc.ErrNotResumed`: the job stays interrupted, its error naming the
  package it was at and how many packages before it were updated.
- After a reboot it is not resumed: PackageKit's transaction does not outlive
  the boot, and continuing the batch would start package work after a reboot
  nobody asked to continue. The job ends `interrupted/reboot`, as before.
- Nor after a stop of the host by hand (`systemctl stop`, `restart`), which an
  admin may do to halt an update: the job ends `interrupted/shutdown`, as
  before, while PackageKit finishes the package it was at.
- Cancel, in the first run or after a pick-up, asks PackageKit to cancel the
  transaction the job follows and updates no further package. A host stop or
  crash does not cancel the transaction, which runs on.

`storage.run_smart_test` resumes after a reboot, and in the same boot after a
crash or a stop by hand of its host
(`backend/bridge/handlers/storage/smart_test_operation.go`). The self-test
runs inside the drive, which outlives the job host, and the drive keeps a log
of its self-tests across power cycles; what the job saves and how it reads the
drive again is in [Storage Disks](./storage-disks.md#smart-self-tests). In
short: before it starts the test the job saves who the drive is and its
self-test log, and after `smartctl -t` that it started it. `Resume` takes the
drive only when `/dev/<name>` reports the same model, serial number and WWN
and the start was saved; it follows a test the drive still runs, or takes the
one ended test of the job's kind that the log gained since as the outcome,
and never starts a test. Anything else, a test a reset interrupted included,
leaves the job interrupted. Cancel aborts the test in the drive when it is the
job's; a host stop does not.

The long ioctls run on a locked OS thread with async signals blocked: since
kernel 6.19 any pending signal aborts a scrub or a replace, and an aborted
replace loses its target, so retrying is not an option. A freeze (a suspend,
`fsfreeze`, a frozen cgroup) still interrupts them: the scrub issues its call
again and continues, the replace fails.

`docker.apply_daemon_storage` is not resumable but is written so that a dying
job cannot leave Docker down: it writes `daemon.json` and the data-root drop-in
while Docker runs and asks systemd for one restart of `docker.service`, which
systemd finishes whatever happens to the job, then waits up to three minutes
for a new invocation. Stopping and starting Docker as two steps of the job
would leave Docker and every container down if the job died in between, since
systemd never restarts a deliberately stopped unit, and a checkpoint would not
help: `Resume` follows work that runs on outside the job, and once the job had
stopped Docker nothing would run on to follow.

## What each event does

| Event | What happens to a job |
|---|---|
| Page reload, tab close, connection loss | The stream detaches and the job runs on; the client resends or reattaches once ([The mutation stream](#the-mutation-stream)) |
| Logout, session expiry, bridge exit | The bridge closes its host connection; jobs run on. Any later login of the uid sees them |
| Host unreachable from the bridge | New mutations fail with `503`; job reads wait up to 10 s, then answer `503`; the bridge reconnects with backoff |
| Host of an older build | New mutations the host would run are refused with `409` `update_pending: …`; the older host finishes its jobs ([Updates](#updates)) |
| `systemctl stop`, orderly shutdown | `interrupted/shutdown`, a resumable job after its `Suspend` stored a checkpoint; a job with `cancel_requested` ends `canceled`. The root host raises a warning. A stop by hand in this boot: the next root host picks up a job of a route that resumes after a stop (`storage.run_smart_test`; not `packages.update`) |
| Host crash, `kill -9`, OOM kill of the host | The next host of the uid marks the job `interrupted/crash`. The root host picks up a job of a route that resumes after a crash (`packages.update`, `storage.run_smart_test`), unless the crashed run was itself a pick-up, and alerts on any other, which is never resumed |
| OOM kill of a job's subprocess | That job fails; the host and its other jobs run on |
| Reboot or power loss | The root host, started at boot, marks the records a power loss left running `interrupted/reboot`, resumes the resumable jobs the shutdown or the power loss interrupted, and raises a warning for each job it marks and does not resume. A user's host marks theirs at the next login |
| Cancel (`tasks.cancel`, stream abort) | `cancel_requested`, then the handler cleans up, then `canceled` |
| LinuxIO update | Nothing: hosts are outside `linuxio.target` and the installer never stops them |

## Updates

The installer replaces the binaries and may reload systemd, but never stops
or restarts a job host: an old host finishes its jobs and exits under its
normal rule, and the next connection starts the new binary.

The hello carries `jobs.Protocol`. A host of another protocol refuses it, and
to the bridge that is an older host an update waits for. A refused hello does
not count as a connected bridge, so the older host exits 60 s after its last
job and its last bridge of its own build, and the bridge's next attempt, at
most 30 s later, starts the new host. Until then:

- New mutations the host would run are refused with `409` and a message that
  starts `update_pending:` and names how many jobs the update waits for (it
  says new jobs can start shortly when none runs, and names no count when the
  store cannot be read). The three browser-bound mutations run in the bridge
  and are not affected, nor are Calls and channels that do not go to the host.
- The bridge cannot ask the older host anything, so it reads that host's
  store itself, read-only, as the same uid: `tasks.host` lists the `running`
  records of executor kind `host` whose process still lives, with their last
  progress and without their paths and package IDs.
- `tasks.history` and `tasks.list` answer without the host's jobs, and
  `tasks.events` sends none; `tasks.get`, `tasks.watch`, `tasks.cancel` and
  `tasks.output` of a host job wait 10 s and answer `503`. The table is in
  [API Contract](./api-contract.md#ended-jobs-saved-output-and-updates).

There is no button to force the update: forcing means stopping the old host,
which interrupts the jobs it runs. Letting a new host start beside the old one,
so that new jobs never wait, would remove the wait and is not built.

On Ubuntu, needrestart, which apt runs after every dpkg run, restarts each
service still running a replaced binary, and so would restart the job hosts
and interrupt the jobs an update waits for. The installer therefore installs
`packaging/etc/needrestart/conf.d/linuxio.conf`, which keeps needrestart from
restarting the job hosts, the timer-run units and the updater's unit, and
checks the file token by token before it installs it, because needrestart
runs it as Perl, as root
([Process & Systemd Architecture](./process-systemd-architecture.md#login-and-jobs),
needrestart).

## Scheduled runs

Work a systemd timer starts keeps its own unit. It is never handed to a job
host, for three reasons: a script runs as a configured user with an arbitrary
command; the snapshot, replication, resync and Docker update units work and
alert on their own; and no verb starts a job from outside the browser. Each
run instead writes its own record from two hooks on its unit
(`backend/common/scheduledrun`, `backend/worker/scheduled_run.go`).

The hooks and LinuxIO's own timer jobs are words of one program,
`linuxio-worker`, which only systemd starts and only root may run
([Process & Systemd Architecture](./process-systemd-architecture.md#binary-invocation-map)).
They write the stores themselves, through the shared packages, and do not ask
a job host over its socket: a hook fires at boot, during shutdown and in the
middle of an update, when no host is sure to be up or of the same build,
while a file write needs nothing else running. The same holds for the timer
jobs, which also keep their own unit, limits and journal that way. The units:

| Record `route` | Unit | Label | Page |
|---|---|---|---|
| `scheduled.script` | `linuxio-schedule-<id>.service` | the schedule's name | `/services/schedules?schedule=<id>` |
| `scheduled.btrfs_snapshot` | `linuxio-btrfs-snapshot.service` | Snapshots | `/storage/subvolumes?view=snapshots` |
| `scheduled.btrfs_replicate` | `linuxio-btrfs-replicate.service` (started by each snapshot pass with a job enabled) | Replication | `/storage/subvolumes?view=replication` |
| `scheduled.btrfs_resync` | `linuxio-btrfs-resync@<uuid>.service` | Pool resync | `/storage?pool=<uuid>` |
| `scheduled.docker_update` | `linuxio-docker-update.service` | Container updates | `/docker/containers` |
| `scheduled.system_update` | `apt-daily-upgrade`, `dnf-automatic`, `dnf5-automatic` or `mintupdate-automation-upgrade.service` | System updates | `/updates` |

`linuxio-btrfs-health.service`, the degraded mount at boot and the indexer's
timer are not recorded.

A pool resync cut short, by a power loss, a shutdown or **Stop check**,
reads interrupted or failed like any run, and its next run, which the health
check starts, continues the scrub from the progress it saved; the job host
never resumes it ([Btrfs](./btrfs.md#missing-disks)).

**Hooks.** `linuxio-worker scheduled-run begin` and `end` run as `ExecStartPre=` and
`ExecStopPost=` of each unit. Where they are installed, why they sit in
packaged drop-ins rather than in a rendered unit, what `+` and `-` do, and the
fields and outcomes they write are in
[Process & Systemd Architecture](./process-systemd-architecture.md#scheduled-runs).
The reasons that document does not give:

- `begin` fsyncs its record, so a power loss right after the start cannot take
  the record the boot then marks interrupted.
- `end` writes the outcome, then prunes the unit's older runs, then saves the
  output under its own 5 s limit, then raises the alerts: a slow journal cannot
  starve the prune, and a slow alert delivery cannot keep the output from
  being saved.
- Scheduled runs have a store of their own so they have their own limit, the
  newest 50 ended runs per unit and none older than 30 days, and every run is
  kept, a run shorter than a second included.
- The output is the run's journal lines rather than log files of LinuxIO's
  own, because the Logs page would lose the output and size limits and
  clean-up would be LinuxIO's to build. `--quiet` keeps an empty run from
  saving `-- No entries --`. Stdout and stderr are one journal stream and
  cannot be told apart; sending stderr through a second journal connection
  lost lines.
- The worker and the Docker updater write their run's
  progress file, found through `$INVOCATION_ID`
  (`scheduledrun.ReporterFromEnv`). Scripts report none; their output is their
  feedback.

**In the job list.** Only the root host reads the scheduled store, so every
admin sees every scheduled run and other users see none; what `tasks.*`
answers for one is in [API Contract](./api-contract.md#scheduled-runs). A
`running` record counts only while systemd runs its invocation in this boot,
checked at host start and at every listing. Of the D-Bus errors only
`NoSuchUnit` means the run is gone; any other leaves the record alone, so a bus
hiccup cannot raise a false alert.

**Stopping.** The bell offers no stop for a scheduled run; its row opens the
owning page. `schedules.stop` marks only the record of the invocation it names,
so a stop that arrives after its run ended cannot mark the next run; a scrub
cancel that hits a timer's resync knows no invocation and marks the resync
unit's running record. Either takes the mark back when the stop or the cancel
fails.

**A timer program's run completes when its items fail.** LinuxIO's own
timer programs (`linuxio-worker` for snapshots, replication and resync, and
the Docker updater) raise an alert of their own for each
rule, job, pool or container that fails, and the run did its work, so the
program exits with 0: the unit is not failed on the Services page and the run
ends `completed`. It also exits with 0 for a run that did no work, such as a
Docker pass that found a manual update holding the lock or a replication run
whose every job another run held, and with 4 (`scheduledrun.ExitSkipped`) for
such a run only when its summary could not be stored, since the end hook
would otherwise read it as a run that ran. It exits with 1, and gets one run
alert per unit from its end hook, for a failure no alert of its own
reports: configuration that cannot be read, a Docker daemon that does not
answer, a scheduled container that is gone, a replication that could not be
started, a resync that could not find its pool. It also exits with 1 when it
was stopped, by a signal or, for a resync, by a scrub cancel, whatever its
failures were marked: a run that exited with 0 would read as completed, and
the end hook could not record it as canceled or interrupted. And it exits
with 1 when items failed and its summary could not be stored (the store's
lock held for more than 5 seconds, a full disk): the summary is the only place
a completed run counts its failures, so without it the history would show a
plain Completed. The run then reads failed, and its run alert points to the
output, where the failures are listed. A run whose items all succeeded still
completes when its summary is lost.
`scheduledrun.Conclude` decides the status for both programs. A system update's failure is reported by
the provider's notify hook where it is installed, and otherwise by the end
hook.

What the run did is in two places:

- **The record's result** is a summary (`scheduledrun.Summary`), stored by
  a run that completes: `message`, `failures` (how many items failed with an
  alert of their own) and `skipped`. The history shows the message where it would say Completed, with
  the amber marker when `failures` is not zero. The end hook reads `skipped`:
  such a run proves nothing about the unit, so it leaves a standing run
  alert, where any other completed run clears it. The messages:

  | Worker | Messages |
  |---|---|
  | Docker updates | *1 updated, 1 failed, 1 rolled back*, *2 waiting*, *Nothing to update*, *Skipped: another Docker update was running* |
  | Snapshots (the timer's pass) | *2 taken, 3 pruned, 1 failed*, *Nothing to do* |
  | Replication | *1 sent, 1 failed*, *Nothing to do*, *Skipped: another run held every job* |
  | Pool resync | *Resynced*, *Resync failed* (the pool's alerts say why, and the health run retries it) |

  The snapshot pass before a system update runs inside that update's unit
  and stores none; a rule that fails there exits with 1.
- **The run's output.** The Docker update pass ends its output with a report
  (`PassReport.Write` in `backend/common/dockerupdate/pass_report.go`): a
  line of totals, then one line per container it replaced, with the image,
  the versions when both images carry the `org.opencontainers.image.version`
  label and the short image IDs, or failed to, with the reason, or rolled
  back. The saved output shows a log line's message and not its journal
  fields, so the lines of the pass about one container end their message
  with its name ([Docker Updates](./docker-updates.md#the-scheduled-pass)).
  The worker's btrfs jobs print a line of counts and each failure.

## Alerts

Only the root host raises alerts and reads the notification settings, because
the alert store is root-owned; a user's host writes records only. The levels,
keys, messages and settings are in
[Alerts](./alerts.md#who-writes-with-no-session); in short:

- A `failed` job raises an alert with its error. A job is named by its label,
  a file-browser job as `<Action>: <label>`, and one without a label by its
  route; the alert links to `/history?job=<id>`.
- An interruption by a shutdown or a reboot is a warning. One by a crash, a job
  that could not be resumed, and a job found with no task to finish it are
  alerts: their outcome is unknown, a failure in effect, so "failures are
  always reported" stays whole.
- A resumed job that completes clears the alert of its interruption. A job
  picked up in the same boot whose `Resume` found nothing to follow raises the
  alert its interruption would have raised, with the reason.
- A `completed` job that ran five minutes or more raises a notice when the
  settings ask for long jobs. A `canceled` job raises nothing.
- Scheduled scripts raise the same jobs alerts from their end hook, or from the
  root host when it finds a run ended without that hook. When the settings ask
  for every run, a schedule's completed runs raise one notice per schedule,
  each run a new occurrence of it, because one alert per run would push
  undismissed failures out of the 200-record alert store.

## Security

- Authorization is the uid of the connecting process, read by the kernel
  (`SO_PEERCRED`). A user host accepts only its uid, the root host only uid 0,
  and only the root host's sessions are privileged. The socket is `0600` and
  owned by its uid.
- `linuxio-auth` starts a host's socket only for the uid PAM admitted.
- A record holds the request without its secret fields, is `0600` in a `0700`
  directory owned by the uid, and is never returned by an API: snapshots carry
  route, target, owner, state, progress, result, error and times.
- Both kinds of host run with `NoNewPrivileges=yes`.
- `tasks.output` is `Privileged()`, and only the root host keeps saved
  outputs.
- On the root host every admin sees every admin's jobs, their errors and
  metadata included: admins are root-equivalent, and `tasks.get` returns any
  job by its ID.

## Frontend

- **Bell.** The tray lists running jobs from `tasks.events`, of the types
  `useRecoveredTasks` knows (a type that is not listed there gets no row),
  with their progress, and running scheduled runs with their label and
  progress capped
  at 99 %, so a row never reads as complete. A scheduled run's row has an
  `Open` link to its page where other rows have **Cancel task**, since its
  cancel is refused. A job being cancelled reads "Cancelling…" until it ends.
  The tray ends with a `History` link. The upload and download engines are in
  [Frontend Task State](./frontend-task-state-unification.md).
- **A dialog that starts a long job does not hold itself open.** A job that
  reports progress runs on when its dialog is closed. A dialog whose write
  takes a moment stays held while it is pending: it is where the error and
  the retry are. The Memory overflow dialog
  ([Memory overflow](./memory.md#the-dialog)) is the reference. What that
  takes:
  - **Closing.** Close, Escape and the backdrop work; the controls that would
    start a second job stay held. `AppConfirmDialog` takes
    `closeWhilePending` for a confirm whose action is such a job. A dialog
    may instead close as its job starts, as the file browser's do, and then
    holds nothing, so any number of jobs of a kind can run at once. A closing
    dialog fades out with the handlers it had, so its confirm handler acts
    only while the dialog is still open: a second click during the fade does
    not start the job again.
  - **Each job reports itself.** One `useMutation` can follow several jobs:
    each `mutate` builds its own TanStack mutation, and the hook's `success`
    and `error` run once for each job with that job's request. `isPending`,
    and callbacks passed to `mutate` itself, follow only the latest job, so
    they neither hold a dialog nor report a job.
  - **A tray row.** The tray lists a job only when its type is in
    `useRecoveredTasks` (`frontend/src/tasks/engine/useRecoveredTasks.ts`),
    which also gives the row its text. A job type that is not there has no
    row, and so no progress and no cancel once its dialog is closed.
  - **One report.** The mutation toasts the result, and its toast does not
    depend on the dialog being open. It marks the job reported when it starts
    (`markTerminalFeedbackEmitted`), so the tray does not toast its end as
    well. For a job nobody follows, as after a reload, the tray reports from
    the type's entry in `TERMINAL_TASK_FEEDBACK`.
  - **A cancel is not a failure.** The tray that cancelled the job says so.
    A Mutation's `error` text is not toasted for a cancel
    (`mutation-react-query.ts`), and a page with an error handler of its own checks
    `isTaskCancellationError`. A cancelled delete or move to Trash has
    removed some of its items, so it reads the listing again and deselects
    them as a finished one does; a cancelled restore or purge from the Trash
    reads its lists again.
  - **The end of a job leaves later work alone.** Its completion does not
    close or clear a dialog the user has since closed and opened again for
    something else, nor a selection made since: a delete or a move to Trash
    deselects only its own items.
  - **Taking a job up again.** A page that shows the job's step, or must
    hold its controls, finds a running job when it opens
    (`useActiveTaskRecovery`, then the mutation's `watch`), and counts running
    mutations in the cache (`useIsMutating` with a `mutationKey`) so that a
    job an earlier instance still follows is not followed twice.

  The dialogs that work this way: the Memory overflow dialog; the Docker
  storage confirm in Settings, whose section shows the running step in a note
  once the confirm is closed; and the file browser's upload dialog, which is
  held while it checks the destination and asks about conflicts, which need
  it, and can be closed once the upload runs; an upload has its own tray row,
  and a dialog opened again can start another upload beside it. The file
  browser's delete confirm, Permissions dialog and the Trash's confirms,
  properties and Restore to… dialogs close as their job starts, as in Windows
  Explorer, and the Trash's Restore, Delete permanently and Empty Trash take
  the job's items out of its selection as it starts
  ([Trash](./trash.md#the-trash-view)); any number of these jobs can run at
  once, each with its own tray row and its own result toast. A question a job
  raises when it ends (delete for good what has no trash, what to do with a
  restored name that is taken) is the page's, not a dialog held for the job. A
  permission change that fails for some items offers **Try again** in its
  toast while the file browser shows: it opens the Permissions dialog for
  those items with the mode, owner, group and recursive setting the change
  was made with.
- **History page** (`/history`, open to every signed-in user and not in the
  sidebar; `frontend/src/routes/_authenticated/history`). It reads
  `tasks.history` and lists ended jobs and scheduled runs, newest first: when
  each ended, its title, its outcome (`Completed`, `Failed`, `Cancelled`,
  `Interrupted (<reason>)`), who started it and how long it ran. The title is
  the job's label; a file job is named in the bell's words (`Copying 12 items`,
  `Moving report.pdf to Trash`; `frontend/src/tasks/taskTitles.ts`), and a job
  without a label from its route. An `Outcome` filter narrows the loaded
  entries. `Load older` asks for the next page with the last entry's end time
  exactly as received: the server's times carry nanoseconds that a JavaScript
  `Date` would drop, and a page never splits entries that ended at the same
  instant. Selecting a row (`?job=<id>`) shows its error, an `Open` link for a
  scheduled run and, for a privileged session, its saved output; a job outside
  the loaded pages says so. Admins see the root host's jobs and every
  scheduled run; other users see their own host's jobs.
- **Saved output** (`frontend/src/components/tasks/SavedOutput.tsx`). The
  viewer reads the last 256 KiB through `tasks.output` and opens scrolled to
  its end; `Show earlier` reads the part before, which joins above while the
  text in view stays where it was. The output is shown as plain text.
- **Past runs.** Each schedule on the Schedules page lists its runs
  (`tasks.history` with its unit; 50 runs fit one page), newest first;
  selecting one shows its error and its saved output. The live card *Output of
  the latest run* follows the journal instead.
- **Update banner** (`JobsUpdateBanner`, in the main layout for every
  signed-in user). It asks `tasks.host` when the connection opens, every 15 s
  while an update waits, and at once after a mutation fails with
  `update_pending`; that mutation's default toast drops the prefix. The banner
  is a `role="status"` region: `LinuxIO was updated. New jobs wait for N
  running jobs to finish.`, with `Show running jobs` listing each job's title,
  who started it, when, and its last percentage; with none running, `LinuxIO
  was updated. New jobs can start shortly.` It names the jobs itself because
  the bell cannot show a job of an older host. While it waits and the history
  is empty, the history page and `Past runs` say `History is unavailable until
  the LinuxIO update finishes.`, and when the wait ends the banner refetches
  every `tasks.history` query once, so both fill without a reload.
- **Notice at sign-in.** Right after a sign-in an admin hears how many alerts
  are unseen and another user how many of their own jobs failed or were
  interrupted since they last saw the history ([Alerts](./alerts.md#navbar)).

## Logs

A job host logs to journald under `linuxio-jobs`, in its unit's journal
(`journalctl -u linuxio-jobs@<uid>.service`); a scheduled run's lines are its
own unit's. The task service's lines name a job by `task_id` and the host's
own lines by `job_id` (journald fields `LINUXIO_TASK_ID` and
`LINUXIO_JOB_ID`). Lines worth knowing when a job did not finish:

| Line | Level | Meaning |
|---|---|---|
| `task failed` | Error, Warn for a `4xx` | The handler returned an error, given as `error` |
| `task interrupted`, `task canceled` | Info | The handler ended under a host stop or a cancel |
| `job marked interrupted` | Info | A host start or a request marked a record, with its `reason` |
| `job suspend failed` | Warn | A `Suspend` hook failed at shutdown; the job is interrupted without a checkpoint |
| `job host stopped before a job ended` | Warn | The 100 s shutdown bound ran out with that job still running |
| `job resumed` / `interrupted job not resumed` | Info / Warn | A host start's resume or pick-up of a job, or why the host could not start it; a `Resume` that found nothing to follow logs `task interrupted` with why |
| `job host refused a bridge hello` | Warn | `protocol N, want M`: an update waits for this host |
| `job host refused a connection` | Warn | A peer of another uid |
| `job host refused a job` | Warn | The host is stopping (`503`) or full (`409`) |
| `job record not created`, `job record not finished` | Error | The store could not be written; the job ran without its record |
| `scheduled run marked interrupted` | Info | The root host found a run its end hook never finished |

The record itself is the quickest evidence: as root,
`/var/lib/linuxio/jobs/<uid>/<id>.json` shows its state, reason, executor and
`cancel_requested`.

## Testing

| Level | Make target | Files | What |
|---|---|---|---|
| Go unit | `make test-go`, in `make check-backend-quiet` | `backend/common/jobs/store_test.go`, `backend/common/jobs/scheduled_test.go`, `backend/common/jobs/process_test.go` | Write policy, quick-job deletion, retention and prune, crash detection by pid and start time, history paging through equal end times, saved output chunks |
| Go unit | same | `backend/bridge/internal/jobshost/host_test.go`, `backend/bridge/internal/jobshost/scheduled_test.go` | Peer uid and protocol refusal, idle exit, SIGTERM and `Suspend`, cancel during shutdown, reconcile and the resume rule (`TestResumeOnlyWhatTheLastShutdownInterrupted`), alerts and their levels, scheduled runs in `tasks.*`, slim history entries, `tasks.output` |
| Go unit | same | `backend/bridge/internal/jobshost/pickup_test.go` | The same-boot pick-up: a crash and a stop picked up before the host serves, a stop not picked up for a route that takes crashes only, none after a reboot, a cancel, a lost job or a crash of a picked-up run; resumes after a reboot started once the host serves, and left as they were by a stop during the start; a `Resume` that finds nothing left interrupted with its reason and alert and not tried again, and one during a stop counted as the stop's; a failed resume stamped, made unresumable only for a route that resumes in the boot; a checkpoint saved while a job runs |
| Go unit | same | `backend/bridge/handlers/storage/smart_resume_test.go`, `backend/bridge/handlers/storage/smart_test.go` | Against a fake drive (smartctl's JSON report): the drive read and saved before the test starts, a test the drive still runs followed, an ended test read from the log (completed, failed, aborted, a reset), another drive, an unreadable one, no new test, more than one, a test of another kind or a log that does not continue never taken, an NVMe drive running the other kind, Cancel aborting the test after a pick-up and in the first run, and not on a host stop, once the name is another drive's, when `-t` was refused, or when the drive runs another kind or none |
| Go unit | same | `backend/bridge/handlers/packages/package_update_resume_test.go`, `backend/bridge/handlers/packages/packagekit_test.go` | Against a fake PackageKit on a private bus: the checkpoint saved before each package's update, a transaction followed to its end, one that ended read from the history (succeeded, failed), one PackageKit cannot account for or of another role never taken, one dropped before its role is read taken from the history, the batch's counts in the error of a job left interrupted, a non-success exit with no ErrorCode as a failure (an update and an install), Cancel sent to PackageKit after a pick-up and in the first run, refused by PackageKit, and not on a host stop |
| Go unit | same | `backend/bridge/cmd/jobs_relay_test.go`, `backend/bridge/cmd/jobs_host_test.go`, `backend/bridge/internal/jobsclient/client_test.go` | A bridge and a host over a socket: forwarding, host loss, reattach, events after a host restart, the update wait. The relay tests dial the in-process host with `jobsclient.Dial`, which starts no socket unit, so they also run as root |
| Go unit | same | `backend/bridge/cmd/jobs_relay_test.go` (`TestRelayRoundTripSpeed`) | A speed bound: 300 no-op mutations through the relay to the in-process host, each timed from dispatch to its result frame; fails when the median exceeds 50 ms (observed 0.5 to 0.6 ms with the race detector, 0.2 to 0.3 ms without). It logs the median, p95 and maximum, and runs as root too (`make vm-test-relay-root` matches it through `Relay`) |
| Go unit | same | `backend/bridge/internal/bridgeipc/mutation_test.go`, `backend/bridge/handlers/redaction_test.go`, `backend/bridge/apischema/schema_test.go` | The stream, resend and abort, same-target refusal, option guards, secret tags, `TestRetrySafeRoutesAreExplicitCalls` |
| Go unit | same | `backend/common/scheduledrun/*_test.go`, `backend/worker/scheduled_run_test.go` | The hooks' outcomes and order, liveness, output copy, exit statuses, unit kinds |
| C | `make test-auth-pam`, in `make test-backend` | `backend/auth/linuxio-auth-pam_test.c` | The exact `systemctl` arguments of an unprivileged login, before the session opens; a failed, missing or hanging `systemctl` keeps the login; a login the sudo probe admits (a stub `sudo`) is privileged and starts no host |
| Installer | `make test-installation-scripts` | `packaging/scripts/tests/installer-fixtures.sh` | Unit fixtures, the run-record drop-ins, the needrestart override's content check and its six expressions |
| Frontend unit | `make test-frontend`, in `make check-frontend-quiet` | `HistoryPage.test.tsx`, `SavedOutput.test.tsx`, `JobsUpdateBanner.test.tsx`, `useJobHostStatus.test.ts`, `useRefreshHistoryAfterUpdate.test.tsx`, `useAwayNotice.test.tsx`, `NotificationsBell.test.tsx`, `taskTitles.test.ts`, `scheduledRuns.test.ts`, `mutation-react-query.test.ts`, `call-react-query.test.ts` | Pages, banner and bell in jsdom, which has no layout; the client's resend and reattach; the `update_pending` toast |
| Browser | `make test-frontend-browser-quiet` | `frontend/src/test/browser/history.spec.ts`, `frontend/src/test/browser/schedules.spec.ts`, `frontend/src/test/browser/background-tasks.spec.ts` | Real layout against the fixture: the history table has height, the Schedules table keeps its rows beside tall details, tray rendering |
| Root, in the VM | `make vm-test-relay-root` | `backend/bridge/cmd/jobs_relay_test.go` | The relay tests as root: the in-process host is the root host, so its owner and privileged rules differ from a user host's |
| Root kernel | `make test-btrfs-integration` (sudo, the user's to run); `make vm-test-btrfs-integration` in the VM | `backend/common/btrfs/integration_resume_test.go` (`TestBtrfsResumeIntegration`) | Kill and resume each btrfs operation on loop devices: signals, cancels, kills, a paused balance across a remount, a scrub from its offset, a scrub from its saved progress after a kill, a scrub across a filesystem freeze and its cancel there by its context and through `ScrubCancel`, a replace across a simulated power cut |
| Root kernel | same | `backend/common/btrfs/degraded_integration_test.go` (`TestBtrfsDegradedIntegration`) | A pool resync cut by a simulated power cut mid-scrub (a frozen copy of the disks and the state), booted from the copies and continued by the next resync, with corrections on both sides of the cut; a resync during whose scrub, then whose convert, one disk's writes fail through a device-mapper error target fails naming it, and the next one makes that disk whole |
| Test VM | `make vm-reset`, `make vm-install`, `make test-vm-browser-rerun` | `frontend/src/test/vm/jobs-phase2.spec.ts` | Reloads mid package update, Compose and copy; a package update picked up from PackageKit after a `kill -9` of the root host during dpkg; uploads; a scrub across logout; host stop, kill, reboot and power cut |
| Test VM | same | `frontend/src/test/vm/jobs-phase3.spec.ts` | Balance, scrub and replace across stops, reboots and power cuts; a scrub across a frozen job host; a cancel or a crash before a reboot; pool create rollback; the Docker storage switch killed |
| Test VM | same | `frontend/src/test/vm/jobs-phase4.spec.ts` | Scheduled runs: records, bell rows, saved output, stop, a broken hook, the snapshot, Docker and system update units, a power cut and an orderly reboot |
| Test VM | same | `frontend/src/test/vm/jobs-phase5a.spec.ts`, `frontend/src/test/vm/jobs-needrestart.spec.ts` | History, past runs, the output viewer, a user without sudo, the update banner; needrestart deferring a job host |
| Test VM | same | `frontend/src/test/vm/jobs-phase5b.spec.ts` | Notification settings, the grid at delivery and in the bell, a schedule's choice, an interruption as a warning, the notice at sign-in |
| Test VM | `make test-vm-browser` runs it last, on a VM reverted to clean | `frontend/src/test/vm/jobs-update.spec.ts` | A real update from the published release v0.53.0 while two root jobs and a plain user's job run: the banner and `tasks.host`, `tasks.watch`, `tasks.cancel` and `tasks.output` during the wait, uploads, the update proceeding after the last job, the older host's records read by the new one |

Run each VM spec file on a fresh install, with `login.spec.ts`
([Development](./development.md#test-vm)). The VM signs in as a sudo user, so
its jobs run in the root host, except where a check creates a user without
sudo. The evidence is the record, read over SSH, and the host's journal.

The update checks install an update build: this tree with `jobs.Protocol` one
higher, so a host of the installed build is an older one to the new bridge.
`make vm-update-build` makes it in `.cache/vm-update-build/build` without
touching the tree: a copy of the plain build whose Go binaries are built
again with a Go overlay of `backend/common/jobs/record.go`.
`make test-vm-browser` builds it and passes it in `LINUXIO_VM_UPDATE_BUILD`;
without it both checks are skipped. The update banner check in
`jobs-phase5a.spec.ts` installs it over the running build and leaves it
installed for the files after it on that VM, a build that differs only in the
protocol. By hand:

```sh
make vm-reset vm-install vm-update-build
LINUXIO_VM_UPDATE_BUILD=.cache/vm-update-build/build make test-vm-browser-rerun PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5a.spec.ts'
```

`jobs-update.spec.ts` downloads v0.53.0 (the first release with job hosts)
into `.cache/vm-release/v0.53.0`, checks it against the release's
`SHA256SUMS` and installs it with the rig's install and that release's own
installer and packaging, which the rig takes from the `v0.53.0` git tag
(`linuxio-vm.sh install BUILD_DIR REF`): this checkout's units start
programs and words the release does not have. It then installs the update
build with this checkout's installer, as an update does, which also runs the
rewrite of the saved unit commands. v0.53.0 has the same `jobs.Protocol` as
the tree, so only the update build makes its hosts older. The file is one
serial group sharing one update, needs `vdd` empty, and leaves the VM changed
(the update build installed, an ext4 on `vdd`, a user without sudo, a
schedule), so it is the Playwright project `release-update` of its own:
`make test-vm-browser` runs it after the other files, on its first VM
reverted to clean, with its log in
`.cache/test-logs/test-vm-browser-release-update.log`. By hand:

```sh
make vm-reset vm-update-build
LINUXIO_VM_UPDATE_BUILD=.cache/vm-update-build/build make test-vm-browser-rerun VMS=1 PLAYWRIGHT_ARGS=--project=release-update
```

### Baseline timings

Measured on 2026-11-15 in the test VM (Ubuntu 26.04.1, kernel
7.0.0-38-generic, 4 CPUs, 3.8 GiB memory) right after `make vm-reset` and
`make vm-install`, signed in as the sudo test user, so the calls go through
the root bridge and its job host. A temporary Playwright spec (not kept) opened
one WebSocket to the webserver, as the app does, sent each call on a stream of
its own one after another (5 unmeasured calls first) and timed it with
`performance.now()` in Chromium, whose resolution here is 0.1 ms: from sending
the open frame to the first frame back and to the result frame. The times
include the webserver and the browser's WebSocket.

| What | Route | Calls | Median to first frame | Median to result | p95 to result |
|---|---|---|---|---|---|
| Cheap read | `system.get_server_time` | 200 | 0.3 ms | 0.3 ms | 0.5 ms |
| Cheap read | `config.get` | 200 | 0.3 ms | 0.3 ms | 0.4 ms |
| Job start (cheapest privileged mutation, runs in the root host) | `alerts.mark_all_seen` | 50 | 1.0 ms | 8.3 ms | 9.5 ms |
| Alert listing (the bell) | `alerts.list` | 100 | 0.4 ms | 0.4 ms | 0.7 ms |
| Schedule status, no schedules | `schedules.list` | 50 | 0.3 ms | 0.4 ms | 0.5 ms |
| Schedule status, 25 schedules | `schedules.list` | 50 | 32.4 ms | 32.5 ms | 38.9 ms |

The first frame of a mutation is the host's snapshot of the new job, so "to
first frame" is the job start; the rest of its time is the route (here a
write of the alert store). `schedules.list` reads the unit properties of each
schedule from systemd, about 1.3 ms more per schedule. The 25 schedules were
created through `schedules.create` on a calendar of 2099 and never ran.

These are one VM's numbers on one day, a baseline to compare a later
measurement against and not a limit: the only bound a test enforces is the
relay's (`TestRelayRoundTripSpeed`).

Covered at one level only:

- Unit tests only: the SMART self-test and its pick-up (the test VMs' virtio
  disks have no SMART, and the route accepts only `sd`, `hd` and `nvme`
  names), the email channel, DNF and Mint (no such VM), the Docker
  update producers with containers to update, `scheduled.btrfs_resync` (no
  check returns a missing disk), the 50-run and 30-day prune, the `409`s of
  `tasks.cancel` and `tasks.watch` for a scheduled run, a stale or failed stop,
  `Load older`, and the long-job notice.
- The update wait was run in the VM as a real update from the published
  release v0.53.0 to a protocol-bumped build of the same tree
  (`jobs-update.spec.ts`), since no published release has a higher
  `jobs.Protocol`. The updater's own path (`control.app_update`, which installs
  a published release only, with `--defer-restart` and a restart of
  `linuxio.target`) was not run, nor was `tasks.watch` streaming a running job
  or a cancel taking effect during the wait: nothing can cancel those jobs.
- The fsync of `begin` is read from the source; the power-cut checks run
  `sync` first, because a job's first record write is not fsynced.
- An in-app update that installs the needrestart override is covered by the
  installer fixtures, not by a run.
- A scrub across a frozen cgroup runs only in the test VM, on its kernel; the
  root tests, which also run on the owner's machine, freeze the filesystem
  with `FIFREEZE` instead. No test suspends to RAM: the test VM wakes at once
  from every suspend after the first of a boot.

## Known limits

- A job that completes in under a second keeps no record, so a resend after
  the 30-minute memory sweep runs it again; the browser resends only right
  after a start it lost.
- A job's first record write is not fsynced: a power loss within the kernel's
  writeback can lose the record of a job that had just started, and that job
  is then never reported.
- The 256-job ceiling is checked before dispatch, so concurrent starts can pass
  it by a few.
- A record whose error message alone exceeds 1 MiB still fails to be written
  when the job ends.
- On the root host a live snapshot's `owner.uid` is 0 while the record holds
  the admin's uid; clients read `owner.username`.
- While an update waits, the bell lists none of the jobs waited for and nothing
  can cancel them, the history reads as unavailable, `tasks.host` shows no
  `cancel_requested`, and each 15 s poll rereads the store, so a corrupt
  record logs a warning each time. After the older host exits, `tasks.host`
  still reports the wait until the bridge's next attempt, at most 30 s later.
- A job host of the same protocol built before a route was added answers that
  route `route not found`; there is no fallback.
- With a job host that cannot start, the history page shows an error after
  10 s rather than an empty list.
- A scheduled run shorter than one poll (2 s) may never show in the bell.
- A scheduled run's output is saved from the journal when the run ends, so
  the lines journald's retention removed while it ran are not in it.
- A scheduled run's cancel mark is one flag per run, so two requests can cross:
  a stop that reached systemd but whose reply was lost takes its mark back, and
  the run reads `failed`. A run neither hook could record goes unseen; one
  that `end` could not write is found by the root host's liveness check when
  `begin` wrote it.
- A host killed after the Docker storage switch wrote its files but before it
  asked for the restart leaves Docker on its old settings; the job is marked
  interrupted and alerted, and applying again finishes the switch.
- A resumed scrub, a pool resync's continued scrub, or a scrub call issued
  again after a freeze, counts errors in the dev extent where each device
  stopped twice, and a power loss loses up to a minute of a scrub's progress.
- PackageKit's history keeps no error text, so a package whose transaction
  failed while no job host followed it reads `PackageKit recorded transaction
  … as failed`; a transaction further back than the newest 200 entries of the
  history is not found, and its job stays interrupted.
- A job whose picked-up run crashes its host again is not picked up a second
  time and stays `interrupted/crash`, whatever the cause of that crash.
- Cancel of a package update is refused by PackageKit while dpkg runs: the
  job ends canceled, that package's update completes and no further package
  is updated.
- After a crash of its host the rest of a package batch is updated at the
  next start of the root host; Cancel, not a kill of the host, is how a batch
  is stopped.
- A host that dies between saving a package's transaction and asking
  PackageKit for it, or during the pre-update snapshot, leaves the rest of
  the batch not updated: the job stays interrupted, its error naming the
  package it was at and how many before it were updated.
- A transaction PackageKit cancelled to make way for a higher-priority one
  reads as a failed package.
- **Try again** on a permission change's failure toast opens the dialog of
  the file browser page that started the change, so once the user has left
  that page, as by leaving the file browser and coming back while the change
  ran, it does nothing.

## Not built

- Resuming non-storage work after a reboot, resuming at any start of the root
  host but the one right after the interruption, and a manual resume.
- A queue, per-user or per-route caps, a runtime cap and a default deadline.
- Starting a job from outside the browser: no CLI verb, no timer-started job.
- Timer jobs and hooks that ask the root job host over its socket instead of
  writing the stores themselves (the reasons are under
  [Scheduled runs](#scheduled-runs)).
- A second job host of a new build beside the old one, and a button that
  forces an update.
- Listing or cancelling, from the bell, the jobs an update waits for.
- Saved output for jobs started in the browser: only scheduled runs keep it.
- Stdout and stderr apart in a scheduled run's output.
- Alerts from a user's job host; that user hears of their jobs through the
  notice at sign-in.
- Continuing a device replace that a freeze or suspend-to-RAM interrupted:
  since kernel 6.19 it ends, and its target is lost.
