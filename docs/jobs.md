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
- **`.Resumable(apischema.ResumeHooks{Suspend, Resume})`** lets a job continue
  after a shutdown or a power loss. The binding panics unless the route is
  `Privileged()` (resumption runs in the root host after a reboot) and has no
  secret fields (the resumed handler needs the whole request). Only the four
  btrfs operations declare it ([Resume hooks](#resume-hooks)); replication is
  not resumable, since an interrupted run is reported and the timer catches
  up.
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
| `resumable`, `checkpoint` | Whether the route can resume it, and the opaque state its `Suspend` hook returned |
| `cancel_requested` | Set before a cancel reaches the handler |
| `protocol` | The `jobs.Protocol` of the build that wrote it |

Unknown members are ignored when a record is read, so an older binary keeps a
record that a newer one wrote with members it does not know.

**Write policy.** A record is written when the job starts and on each change,
by rename. It is fsynced only when a job that ran longer than one second
reaches a terminal state, or when its checkpoint or its cancel request
changes: a record write was measured at about 1.4 ms with an fsync on NVMe
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

The root host then resumes, once, each record that is `interrupted` with
reason `shutdown` or `reboot`, still `resumable`, without `cancel_requested`,
and whose executor ran in an earlier boot. Every other interrupted record (a
crash, a host stopped by hand in this boot, a cancel that overlapped a stop)
loses `resumable` at that start. So a job is resumed only by the boot right
after the interruption, never weeks later by some other reboot, and there is
no manual resume. The resumed job keeps its ID, record and start time and runs
as a new task in this host. A job the host cannot resume raises an alert and
is stamped with this host's executor, so later starts in the same boot leave
it alone. A user's host marks its records the same way when the next login
connects, and never resumes: resumable routes are privileged.

`linuxio-jobs@.service` is ordered after `local-fs.target` and
`linuxio-btrfs-degraded.service`, so pools are mounted, degraded if they must
be, before the root host resumes anything.

### Resume hooks

Four routes are resumable: `storage.btrfs_balance`,
`storage.btrfs_scrub_start`, `storage.btrfs_device_replace` and
`storage.btrfs_device_remove`. What each hook does is in
[Btrfs](./btrfs.md#shutdown-and-resume). The hooks differ because the kernel
treats each operation differently:

- A fatal signal, SIGKILL included, cancels a balance in the kernel and deletes
  its item, so only a pause keeps it; the kernel continues a paused balance at
  the next read-write mount. The hook pauses on every host stop, a stop by hand
  included, rather than only when the system is stopping: a paused balance
  then holds the pool's exclusive operation until the pool is mounted again
  or the balance is resumed by hand.
- A scrub has no kernel state, so the checkpoint is each device's offset.
- The kernel tracks a replace in an item and continues it at mount by device
  ID, so `Resume` follows it and never starts one. Device paths are not stable
  across reboots, so a restart could write to whichever blank disk now has the
  target's name. A `FINISHED` replace counts only when the kernel's
  `time_started` is not before the job's own start.
- The kernel never continues a removal, so `Resume` issues it again.

The long ioctls run on a locked OS thread with async signals blocked: since
kernel 6.19 any pending signal aborts a scrub or a replace, and an aborted
replace loses its target, so retrying is not an option. A freeze still aborts
them.

`docker.apply_daemon_storage` is not resumable but is written so that a dying
job cannot leave Docker down: it writes `daemon.json` and the data-root drop-in
while Docker runs and asks systemd for one restart of `docker.service`, which
systemd finishes whatever happens to the job, then waits up to three minutes
for a new invocation. Stopping and starting Docker as two steps of the job
would leave Docker and every container down if the job died in between, since
systemd never restarts a deliberately stopped unit, and a checkpoint would not
help because `Resume` runs only after a reboot.

## What each event does

| Event | What happens to a job |
|---|---|
| Page reload, tab close, connection loss | The stream detaches and the job runs on; the client resends or reattaches once ([The mutation stream](#the-mutation-stream)) |
| Logout, session expiry, bridge exit | The bridge closes its host connection; jobs run on. Any later login of the uid sees them |
| Host unreachable from the bridge | New mutations fail with `503`; job reads wait up to 10 s, then answer `503`; the bridge reconnects with backoff |
| Host of an older build | New mutations the host would run are refused with `409` `update_pending: …`; the older host finishes its jobs ([Updates](#updates)) |
| `systemctl stop`, orderly shutdown | `interrupted/shutdown`, a resumable job after its `Suspend` stored a checkpoint; a job with `cancel_requested` ends `canceled`. The root host raises a warning |
| Host crash, `kill -9`, OOM kill of the host | The next host of the uid marks the job `interrupted/crash`, never to be resumed. The root host raises an alert |
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
command; the snapshot, resync and Docker update units already worked and
alerted on their own; and no verb starts a job from outside the browser. Each
run instead writes its own record from two hooks on its unit
(`backend/common/scheduledrun`, `backend/cli/scheduled_run.go`):

| Record `route` | Unit | Label | Page |
|---|---|---|---|
| `scheduled.script` | `linuxio-schedule-<id>.service` | the schedule's name | `/services/schedules?schedule=<id>` |
| `scheduled.btrfs_snapshot` | `linuxio-btrfs-snapshot.service` | Snapshots and replication | `/storage/subvolumes?view=snapshots` |
| `scheduled.btrfs_resync` | `linuxio-btrfs-resync@<uuid>.service` | Pool resync | `/storage?pool=<uuid>` |
| `scheduled.docker_update` | `linuxio-docker-update.service` | Container updates | `/docker/containers` |
| `scheduled.system_update` | `apt-daily-upgrade`, `dnf-automatic`, `dnf5-automatic` or `mintupdate-automation-upgrade.service` | System updates | `/updates` |

`linuxio-btrfs-health.service`, the degraded mount at boot and the indexer's
timer are not recorded.

**Hooks.** `linuxio scheduled-run begin` and `end` run as `ExecStartPre=` and
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
- The btrfs timer program and the Docker update runner write their run's
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

**Exit statuses.** A worker (snapshots, resync, Docker updates) that raised an
alert of its own for every failure it met exits with status 3
(`scheduledrun.ExitReported`), and its end hook raises no second alert; one
that did no work, such as a Docker pass that found a manual update holding the
lock, exits with 4 (`ExitSkipped`) and changes no alert. Any other failed run
of a worker gets one run alert per unit from its end hook. A system update's
failure is reported by the provider's notify hook where it is installed, and
otherwise by the end hook.

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
- A resumed job that completes clears the alert of its interruption.
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

- **Bell.** The tray lists running jobs from `tasks.events`, with their
  progress, and running scheduled runs with their label and progress capped
  at 99 %, so a row never reads as complete. A scheduled run's row has an
  `Open` link to its page where other rows have **Cancel task**, since its
  cancel is refused. A job being cancelled reads "Cancelling…" until it ends.
  The tray ends with a `History` link. The upload and download engines are in
  [Frontend Task State](./frontend-task-state-unification.md).
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
| `job resumed` / `interrupted job not resumed` | Info / Warn | The boot's resume of a job, or why it failed |
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
| Go unit | same | `backend/bridge/cmd/jobs_relay_test.go`, `backend/bridge/cmd/jobs_host_test.go`, `backend/bridge/internal/jobsclient/client_test.go` | A bridge and a host over a socket: forwarding, host loss, reattach, events after a host restart, the update wait |
| Go unit | same | `backend/bridge/internal/bridgeipc/mutation_test.go`, `backend/bridge/handlers/redaction_test.go`, `backend/bridge/apischema/schema_test.go` | The stream, resend and abort, same-target refusal, option guards, secret tags, `TestRetrySafeRoutesAreExplicitCalls` |
| Go unit | same | `backend/common/scheduledrun/*_test.go`, `backend/cli/scheduled_run_test.go` | The hooks' outcomes and order, liveness, output copy, exit statuses, unit kinds |
| C | `make test-auth-pam`, in `make test-backend` | `backend/auth/linuxio-auth-pam_test.c` | The exact `systemctl` arguments of an unprivileged login, before the session opens; a failed, missing or hanging `systemctl` keeps the login |
| Installer | `make test-installation-scripts` | `packaging/scripts/tests/installer-fixtures.sh` | Unit fixtures, the run-record drop-ins, the needrestart override's content check and its six expressions |
| Frontend unit | `make test-frontend`, in `make check-frontend-quiet` | `HistoryPage.test.tsx`, `SavedOutput.test.tsx`, `JobsUpdateBanner.test.tsx`, `useJobHostStatus.test.ts`, `useRefreshHistoryAfterUpdate.test.tsx`, `useAwayNotice.test.tsx`, `NotificationsBell.test.tsx`, `taskTitles.test.ts`, `scheduledRuns.test.ts`, `mutation-react-query.test.ts`, `call-react-query.test.ts` | Pages, banner and bell in jsdom, which has no layout; the client's resend and reattach; the `update_pending` toast |
| Browser | `make test-frontend-browser-quiet` | `frontend/src/test/browser/history.spec.ts`, `frontend/src/test/browser/schedules.spec.ts`, `frontend/src/test/browser/background-tasks.spec.ts` | Real layout against the fixture: the history table has height, the Schedules table keeps its rows beside tall details, tray rendering |
| Root kernel | `make test-btrfs-integration` (sudo, the user's to run); `make vm-test-btrfs-integration` in the VM | `backend/common/btrfs/integration_resume_test.go` (`TestBtrfsResumeIntegration`) | Kill and resume each btrfs operation on loop devices: signals, cancels, kills, a paused balance across a remount, a scrub from its offset, a replace across a simulated power cut |
| Test VM | `make vm-reset`, `make vm-install`, `make test-vm-browser` | `frontend/src/test/vm/jobs-phase2.spec.ts` | Reloads mid package update, Compose and copy; uploads; a scrub across logout; host stop, kill, reboot and power cut |
| Test VM | same | `frontend/src/test/vm/jobs-phase3.spec.ts` | Balance, scrub and replace across stops, reboots and power cuts; a cancel or a crash before a reboot; pool create rollback; the Docker storage switch killed |
| Test VM | same | `frontend/src/test/vm/jobs-phase4.spec.ts` | Scheduled runs: records, bell rows, saved output, stop, a broken hook, the snapshot, Docker and system update units, a power cut and an orderly reboot |
| Test VM | same | `frontend/src/test/vm/jobs-phase5a.spec.ts`, `frontend/src/test/vm/jobs-needrestart.spec.ts` | History, past runs, the output viewer, a user without sudo, the update banner; needrestart deferring a job host |
| Test VM | same | `frontend/src/test/vm/jobs-phase5b.spec.ts` | Notification settings, the grid at delivery and in the bell, a schedule's choice, an interruption as a warning, the notice at sign-in |

Run each VM spec file on a fresh install, with `login.spec.ts`
([Development](./development.md#test-vm)). The VM signs in as a sudo user, so
its jobs run in the root host, except where a check creates a user without
sudo. The evidence is the record, read over SSH, and the host's journal.

The update banner check needs a build whose `jobs.Protocol` is one higher,
made without touching the tree through a Go overlay; without
`LINUXIO_VM_UPDATE_BUILD` it is skipped:

```sh
cp backend/common/jobs/record.go <dir>/record.go   # raise Protocol by one in the copy
printf '{"Replace":{"%s":"%s"}}\n' "$PWD/backend/common/jobs/record.go" "<dir>/record.go" > <dir>/overlay.json
make build-nocheck BIN_DIR=<dir>/build GO_BUILD_EXTRA_ENV="GOFLAGS='-buildvcs=false -overlay=<dir>/overlay.json'"
make vm-reset && make vm-install
LINUXIO_VM_UPDATE_BUILD=<dir>/build make test-vm-browser PLAYWRIGHT_ARGS='login.spec.ts jobs-phase5a.spec.ts'
```

Covered at one level only:

- Unit tests only: the email channel, DNF and Mint (no such VM), the Docker
  update producers with containers to update, `scheduled.btrfs_resync` (no
  check returns a missing disk), the 50-run and 30-day prune, the `409`s of
  `tasks.cancel` and `tasks.watch` for a scheduled run, a stale or failed stop,
  `Load older`, and the long-job notice.
- The update wait was checked with a protocol-bumped build of the same tree,
  not a real update from an older release; in the VM only `tasks.get` was seen
  failing during the wait, and the browser-bound uploads during it were not
  run.
- The fsync of `begin` is read from the source; the power-cut checks run
  `sync` first, because a job's first record write is not fsynced.
- An in-app update that installs the needrestart override is covered by the
  installer fixtures, not by a run.

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
- A scheduled run's cancel mark is one flag per run, so two requests can cross:
  a stop that reached systemd but whose reply was lost takes its mark back, and
  the run reads `failed`. A run neither hook could record goes unseen; one
  that `end` could not write is found by the root host's liveness check when
  `begin` wrote it.
- A host killed after the Docker storage switch wrote its files but before it
  asked for the restart leaves Docker on its old settings; the job is marked
  interrupted and alerted, and applying again finishes the switch.
- A resumed scrub counts errors in the extent where it stopped twice, and a
  power loss rescans from the start, since a scrub is checkpointed only at a
  stop.

## Not built

- Resuming non-storage work after a reboot, resuming at any boot but the next
  one, and a manual resume.
- A queue, per-user or per-route caps, a runtime cap and a default deadline.
- Starting a job from outside the browser: no CLI verb, no timer-started job.
- A second job host of a new build beside the old one, and a button that
  forces an update.
- Listing or cancelling, from the bell, the jobs an update waits for.
- Saved output for jobs started in the browser: only scheduled runs keep it.
- Stdout and stderr apart in a scheduled run's output.
- Alerts from a user's job host; that user hears of their jobs through the
  notice at sign-in.
- Handling a freeze or suspend-to-RAM during a scrub or a replace.
