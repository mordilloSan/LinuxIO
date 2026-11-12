# Process & Systemd Architecture

How LinuxIO's processes are wired together by **systemd socket activation**
under one umbrella **`linuxio.target`**. This is the deployment counterpart to
the [Server Yamux Protocol](./server-yamux-protocol.md), which covers what flows
*through* the webserver↔bridge connection once it exists.

## Why Multiple Binaries

LinuxIO uses **privilege separation** (the same shape Cockpit uses): split the system into the smallest pieces that each need a given privilege, so the large, network-facing code never runs as root.

| Concern | Binary | Why it's separate |
|---------|--------|-------------------|
| Serve HTTP/WebSocket to the browser | `linuxio-webserver` | Big attack surface (TLS-less HTTP, untrusted input) → runs **unprivileged**, heavily sandboxed, never root. |
| Authenticate (PAM) and launch a session | `linuxio-auth` | Needs **root** for PAM + privilege drop → kept tiny, written in C, audited, socket-activated per connection. |
| Execute user-facing operations | `linuxio-bridge` | Runs with **exactly the logged-in user's** privileges (or root only when the user is privileged). One process per login. |
| Run mutations that outlive the login | `linuxio-bridge`, as `linuxio-jobs@<uid>.service` | One long-lived job host per uid, as that uid (root for admins), so a change started from the UI does not depend on the login, the browser tab or the bridge staying alive. systemd holds each host's socket and starts the host on a connection. A user's bridge cannot start a system unit, so `linuxio-auth` starts a user's socket once PAM has authenticated them; root starts its own. |
| Maintain the filesystem index | `linuxio-indexer` | Needs broad read access but a narrow write boundary → isolated, sandboxed, and socket activated. |
| Sample and store host metrics | `linuxio-monitoring` | Needs **root** for SMART, hwmon, GPU and the Docker socket, and must keep sampling with no session open → one long-running collector, read-only metrics on a world-readable socket, commands behind a root-only one. |
| Operate/inspect the stack | `linuxio` (CLI) | Convenience wrapper over `systemctl`/`journalctl` → runs as the invoking admin. |

## Binaries

| Binary | Lang | Source | Runs as | Lifetime | Role |
|--------|------|--------|---------|----------|------|
| `linuxio` | Go | `backend/cli/` | invoking user (often via `sudo`); root in the scheduled units' hooks | on-demand | CLI: status, logs, start/stop/restart, verbose, version; records scheduled runs |
| `linuxio-webserver` | Go | `backend/webserver/` | `DynamicUser` + group `linuxio-bridge-socket` | long-running (socket-activated) | HTTP + WebSocket relay; **yamux client** |
| `linuxio-auth` | C | `backend/auth/linuxio-auth.c` | `root` | per auth connection (supervises its bridge) | PAM authentication, sudoers policy query, fork + supervise bridge |
| `linuxio-bridge` | Go | `backend/bridge/` | logged-in user (root only if privileged); the job host as its uid | per login session; the job host per uid, started by a connection to its socket, idle exit | **yamux server**; executes reads and relays mutations to the job host; the job host runs mutations as jobs |
| `linuxio-docker-update` | Go | `backend/docker-update/` | root, sandboxed | per scheduled update run | Docker update worker |
| `linuxio-indexer` | Go | `backend/indexer/` | root, sandboxed | socket activated; exits when idle | Filesystem scanner, SQLite index, local HTTP/SSE API |
| `linuxio-btrfs-timer` | Go | `backend/btrfs-timer/` | root | oneshot, started by systemd timers and the auto-update drop-in | Btrfs timeline/pre-update snapshots and the periodic health check |
| `linuxio-monitoring` | Go | `backend/monitoring/` | root, sandboxed | long-running (no idle exit) | Metrics collector, SQLite history, local HTTP API on two Unix sockets |

These binaries install to `/usr/local/bin/`. The CLI is a thin management
front-end; the remaining processes are independently constrained by their
responsibilities.

### Binary invocation map

Only `linuxio` is an operator administration CLI. The other binaries expose
metadata for diagnostics and narrowly scoped process modes owned by systemd or
the auth/bridge launch protocol.

| Binary | Supported interactive surface | Managed or private surface |
|---|---|---|
| `linuxio` | `status`, `logs`, `start`, `stop`, `restart [--full]`, `verbose enable\|disable\|status` (daemons switch live via SIGHUP reload, open sessions at next login; persists across restarts; also in Settings), `version [--self]`, `help`/`-h`/`--help` | `alert raise\|resolve\|auto-update` for systemd hooks (needs sudo); `scheduled-run begin\|end --unit <unit>`, the hooks of the scheduled units ([Scheduled runs](#scheduled-runs)); hidden `btrfs-snapshot`/`btrfs-health` shims that exec `linuxio-btrfs-timer` for units installed by earlier releases |
| `linuxio-webserver` | `help`/`-h`/`--help`, `version`/`-v`/`--version` | `run [-port N] [-verbose]`; used by `linuxio-webserver.service` |
| `linuxio-auth` | `version`, `--version` | No-argument accepted-socket process; root and systemd only |
| `linuxio-bridge` | `version`, `--version`, `-v` | No-argument session process with inherited fd 3 and auth bootstrap; `linuxio-auth` only. With no arguments and the `linuxio-jobs` socket from systemd, the job host (`linuxio-jobs@<uid>.service`). |
| `linuxio-docker-update` | `help`/`-h`/`--help` | `run [--config PATH]` for the managed timer |
| `linuxio-indexer` | `--help`, `-h`, `--version` | Managed daemon; private `--trigger-index` timer client and `--index-mode` scanner worker |
| `linuxio-monitoring` | `help`/`-h`/`--help`, `version`/`-v`/`--version` | `run [--config PATH] [--verbose]`; used by `linuxio-monitoring.service` |
| `linuxio-btrfs-timer` | `help`/`-h`/`--help` | `snapshot run [--now RFC3339]`, `snapshot pre-update`, `health run`; used by the btrfs snapshot and health units and the pre-update hook |

User lookup in the job host goes through `os/user`, which
resolves SSSD/LDAP users only in a cgo build (the default native build); a
`CGO_ENABLED=0` bridge sees only `/etc/passwd` users.

The private indexer modes are an implementation protocol between the daemon and
its systemd units, not alternate administration paths. Indexer status,
configuration, and indexing remain available through LinuxIO and its HTTP APIs.

## Systemd Units

Core units under `linuxio.target`:

```
linuxio.target                       umbrella; WantedBy=multi-user.target
├─ linuxio-webserver.socket          TCP :8090 (dual-stack) → activates webserver.service
│  └─ linuxio-webserver.service      runs `linuxio-webserver run` (DynamicUser, sandboxed)
├─ linuxio-auth.socket               unix /run/linuxio/auth.sock (Accept=yes) → per-conn instance
│  └─ linuxio-auth@.service          one instance per connection; root; forks+supervises a bridge
├─ linuxio-indexer.socket            unix /run/linuxio/indexer.sock → activates linuxio-indexer.service
│  └─ linuxio-indexer.service        runs the managed daemon; exits when idle
├─ linuxio-indexer-index.timer      periodic request to linuxio-indexer-index.service
│  └─ linuxio-indexer-index.service asks the daemon for a full index
├─ linuxio-btrfs-health.timer       every 10 min; enabled only on hosts with btrfs
│  └─ linuxio-btrfs-health.service  runs `linuxio-btrfs-timer health run`; root
├─ linuxio-monitoring.service        runs `linuxio-monitoring run`; root; owns
│                                    /run/linuxio/monitoring/{api,control}.sock
├─ linuxio-bridge-socket-user.service  oneshot: materializes the linuxio-bridge-socket user/group
└─ linuxio-issue.service             oneshot: updates the login issue/MOTD
```

Outside `linuxio.target`, so restarting LinuxIO never cancels them:

```
linuxio-btrfs-degraded.service       boot oneshot, WantedBy=multi-user.target, enabled only on hosts
                                     with btrfs; mounts fstab pools with a missing disk degraded, and
                                     pools udev holds back for a device replace target normally
                                     (`linuxio-btrfs-timer degraded run`)
linuxio-btrfs-resync@<uuid>.service  started by the health check once a returned disk is back;
                                     runs `linuxio-btrfs-timer resync <uuid>` (scrub, then soft convert);
                                     its unit file carries the scheduled-run hooks itself
linuxio-jobs@<uid>.socket            /run/linuxio/jobs/<uid>.sock, 0600, owned by the uid; started by
                                     linuxio-auth at that user's login and by root for uid 0; a
                                     connection starts the host whenever it is down
└─ linuxio-jobs@<uid>.service        job host for one uid (`linuxio-bridge`, no arguments, User=<uid>);
                                     enabled for uid 0, so the root host starts at boot; exits when idle;
                                     TasksMax=8192, MemoryMax=2G; store in /var/lib/linuxio/jobs/<uid>,
                                     progress in /run/linuxio/jobs/<uid>
```

Key unit facts (see `packaging/systemd/`):

- **`linuxio-webserver.socket`** — `ListenStream=8090`, `BindIPv6Only=both` (one socket answers both the A and AAAA records Avahi publishes). systemd owns the listening fd; the service inherits it.
- **`linuxio-webserver.service`** — `ExecStart=/usr/local/bin/linuxio-webserver run`, `DynamicUser=yes`, `Group=linuxio-bridge-socket`. `StateDirectory=linuxio/webserver` gives the dynamic user private persistent storage for the managed TLS certificate. `RuntimeDirectory=linuxio/webserver` is an indexer activity marker whose lifetime is owned by systemd; `Wants=linuxio-indexer.service` warms the indexer without making it a prerequisite. Extensive hardening: `ProtectSystem=strict`, `PrivateDevices`, `MemoryDenyWriteExecute`, `NoNewPrivileges`, `SystemCallFilter`, `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`, etc.
- **`linuxio-auth.socket`** — `ListenStream=/run/linuxio/auth.sock`, `SocketUser=root`, `SocketGroup=linuxio-bridge-socket`, `SocketMode=0660`, **`Accept=yes`**, `MaxConnections=64`. The group+mode is the access-control boundary: only members of `linuxio-bridge-socket` (i.e. the webserver) may connect. Each accepted connection stays open as the bridge's transport until the session ends (sign-out, or the webserver's 60 min idle or 12 h absolute expiry), so `MaxConnections` caps live sessions host-wide, including those of browsers closed without signing out; concurrent login attempts are capped separately by the webserver (`maxConcurrentLogins`, 8).
- **`linuxio-auth@.service`** — template; one instance per accepted connection, with the connected socket as `StandardInput=socket`. `User=root`. It declares no `TasksMax` or `MemoryMax`: a login is not capped (see below). The bridge inherits its rlimits (`LimitNOFILE=2048`, `LimitCORE=0`).
- **`linuxio-indexer.socket` / `linuxio-indexer.service`** — the `root:root`,
  mode-`0600` Unix socket activates one database/scanner owner. The daemon exits
  after its fixed idle grace while systemd retains the socket. Indexer-backed
  bridge routes are privileged, so the bridge reaches the socket only when the
  authenticated user is sudo-authorized and the bridge remains root. The
  service writes only its canonical config and `/var/lib/linuxio/indexer`
  state. See the [filesystem indexer guide](./indexer.md).
- **`linuxio-monitoring.service`** — `Type=simple`, `User=root`,
  `Restart=on-failure`, `ExecReload` sends `SIGHUP` (as do the webserver and
  indexer units; `linuxio verbose` and `logging.set_debug` rely on it). It is not socket-activated
  and has no idle exit, because history must be sampled whether or not a browser
  is connected. systemd owns `RuntimeDirectory`, `StateDirectory` and
  `ConfigurationDirectory` under `linuxio/monitoring`; `ProtectSystem=strict`
  leaves `/var/lib/linuxio/monitoring` and `/etc/linuxio/monitoring` as the only
  writable paths. The daemon creates both of its sockets itself:
  `/run/linuxio/monitoring/api.sock` (mode `0666`, read-only metrics) and
  `/run/linuxio/monitoring/control.sock` (mode `0600` plus an `SO_PEERCRED`
  uid-0 check, metrics and commands). See the
  [monitoring daemon guide](./monitoring.md).
- **`linuxio-bridge-socket-user.service`** — `Type=oneshot`, `DynamicUser=yes`, `User=linuxio-bridge-socket`, `Before=linuxio-auth.socket`. Its only job is to make the `linuxio-bridge-socket` user/group exist *before* the auth socket is created with that group ownership.
- **`linuxio-jobs@.socket`** — template, instance = uid. `ListenStream=/run/linuxio/jobs/%i.sock`, `SocketUser=%i`, `SocketMode=0600`, `Accept=no`, `FileDescriptorName=linuxio-jobs`, deliberately not `PartOf=linuxio.target`. systemd holds the socket and starts the host on a connection that finds it down: after an idle exit, a crash, an OOM kill of the host process, or an update that replaced its binary. A connection that arrives while the host is exiting waits in the socket's queue and starts the next host. A host that cannot start closes the connections that were waiting, so the clients' backoff paces the next attempt instead of systemd's start limit failing the socket.
- **`linuxio-jobs@.service`** — template, instance = uid. `Type=notify` (READY once it serves, STOPPING on idle exit), `User=%i`, `ExecStart=/usr/local/bin/linuxio-bridge` with no arguments: the binary is the job host when systemd passes it the socket named `linuxio-jobs`, and refuses to run by hand without it. `Wants=` and `After=` its socket. `StateDirectory=linuxio/jobs/%i` and `RuntimeDirectory=linuxio/jobs/%i`, both `0700` and owned by the uid, hold the job store and live progress; `RuntimeDirectoryPreserve=yes`. `TimeoutStopSec=2min`, `KillMode=mixed`, `NoNewPrivileges=yes`, `TasksMax=8192` (room for 256 jobs each waiting on a subprocess tree) and `MemoryMax=2G` with `OOMPolicy=continue`, so an OOM kill of one job's subprocess fails that job instead of stopping the host. Ordered after local filesystems and `linuxio-btrfs-degraded.service`, and deliberately not `PartOf=linuxio.target`, so updates and `linuxio restart --full` never stop a running job. `WantedBy=multi-user.target`, enabled for uid 0 only, so the root host starts at boot. At every start a host marks the jobs its uid's last host left running as interrupted, `reboot` when that host ran in an earlier boot and `crash` otherwise; the root host then resumes the resumable jobs the last shutdown or a power loss interrupted (reason `shutdown` or `reboot`, no `cancel_requested`, an executor from an earlier boot; any other interrupted record loses `Resumable`) and alerts on the others. A host touches only its own uid's store. It accepts a connection only from its own uid (`SO_PEERCRED`) and a hello of its protocol number, stays up while a bridge is connected or a job runs, exits after 60 s with neither, and refuses new jobs above 256 running.
- **Who starts a host** — a connection to its socket. `linuxio-auth` runs `systemctl start --no-block linuxio-jobs@<uid>.socket` as root once PAM has authenticated an unprivileged login, before the PAM session opens and the bridge spawns, so `pam_selinux` cannot hand systemctl the user's context; a failure is logged as a warning and the login proceeds. A privileged bridge starts uid 0's socket itself. A socket stays until reboot, so a user's socket exists only after that user authenticated through LinuxIO, or when root starts it, and only that user can connect to it.
- **tmpfiles** — `/var/lib/linuxio/jobs` and `/run/linuxio/jobs` are `0755 root:root`, so each host's `0700` uid directory below them is reachable.

### The `linuxio-bridge-socket` group trick

The webserver runs as a `DynamicUser` — its uid is **ephemeral** and changes across restarts. You can't grant a moving uid stable access to a root-owned socket. The fix: a stable **group**, `linuxio-bridge-socket`.

- `linuxio-bridge-socket-user.service` materializes the group (ordered before the auth socket).
- `linuxio-auth.socket` is created `root:linuxio-bridge-socket` mode `0660`.
- `linuxio-webserver.service` joins that group via `Group=linuxio-bridge-socket`.

So the (uid-less) webserver can `connect()` to the root-owned auth socket purely through stable group membership.

### Scheduled runs

The units systemd timers start record each run in a job store of their own
([jobs design, section 2.14](./TODO/jobs.md#214-scheduled-runs)): scheduled
scripts (`linuxio-schedule-<id>.service`), btrfs snapshots and replication
(`linuxio-btrfs-snapshot.service`), pool resyncs
(`linuxio-btrfs-resync@<uuid>.service`) and Docker updates
(`linuxio-docker-update.service`). Two hooks do it:

```ini
[Service]
ExecStartPre=-+/usr/local/bin/linuxio scheduled-run begin --unit %n
ExecStopPost=-+/usr/local/bin/linuxio scheduled-run end --unit %n
```

- **Where they are.** Never in a rendered unit: `schedules.run_now` refuses a
  schedule whose unit file differs from what the build renders, so the hooks
  live in packaged files. The installer copies
  `packaging/systemd/linuxio-run-record.conf` to `10-linuxio-run.conf` in
  `/etc/systemd/system/linuxio-schedule-.service.d/` (a prefix directory, so
  every `linuxio-schedule-<id>.service` gets it),
  `linuxio-btrfs-snapshot.service.d/` and `linuxio-docker-update.service.d/`,
  and in the drop-in directories of the distributions' automatic update
  services (`apt-daily-upgrade.service.d/`, `dnf-automatic.service.d/`,
  `dnf5-automatic.service.d/`, `mintupdate-automation-upgrade.service.d/`),
  so a system update is recorded like LinuxIO's own timer runs; a host has
  one of those services at most, and systemd ignores a drop-in of a unit that
  does not exist. Then it reloads systemd. The uninstaller removes the
  drop-in from the update services by its name, with the alert drop-in the
  bridge writes beside it, and leaves an administrator's own drop-ins. `linuxio-btrfs-resync@.service` carries the two lines
  itself. A drop-in alone loads no unit: until a snapshot rule or the Docker
  update settings are saved, that unit stays `not-found`. Schedules saved
  before an update record their runs after it without being saved again.
- **`+` and `-`.** `+` runs the hook as root, outside the unit's `User=` and
  sandbox: a script runs as the user it was given, and the Docker update unit
  has `ProtectSystem=strict`. `-` makes systemd ignore a failed hook, a missing
  `linuxio` binary included, so recording never stops or fails a run. A hook
  reads nothing but `--unit` and the environment systemd gives it
  (`$INVOCATION_ID`, and for `end` `$SERVICE_RESULT`, `$EXIT_CODE` and
  `$EXIT_STATUS`), refuses a unit that is none of the four, and gives itself
  10 s, below the `TimeoutStopSec=15s` of a scheduled script's unit.
- **`begin`** writes the `running` record, with an fsync: the ID is the
  invocation ID written as a UUID, the route and label come from the unit name
  (a script's label is its name in
  `/etc/linuxio/schedules/definitions/<id>.json`), the owner is root and the
  executor `{kind: unit, unit, invocation_id, boot_id}`. It runs as
  `ExecStartPre=`, so its time counts against the unit's start timeout, and a
  schedule's timeout can be as short as 1 s.
- **`end`** writes the outcome: `success` is `completed`; otherwise the run is
  `canceled` when its record carries `cancel_requested`, `interrupted` with
  reason `shutdown` when systemd's `SystemState` reads `stopping`, and
  `failed` with the reason otherwise. A run whose `begin` wrote nothing gets
  its record here. Then, in this order, it prunes the unit's older runs,
  copies the run's journal lines to `<id>.log` beside the record
  (`journalctl --sync`, then `_SYSTEMD_INVOCATION_ID=<id> --all -o
  short-iso`: every line keeps its time, and control characters are kept)
  under a limit of its own of 5 s, and raises the alert of a script that
  failed or was interrupted ([Alerts](./alerts.md)). The alert is raised even
  when the command's 10 s have run out, and its delivery by webhook or mail
  (up to 15 s) can outlast them and the unit's stop timeout; the record, the
  prune and the output are done by then.
- **Stopping.** `schedules.stop` sets `cancel_requested` on the record of the
  invocation it names, then stops that invocation; a stop that arrives after
  its run ended marks nothing. A scrub cancel (`storage.btrfs_scrub_cancel`)
  marks the running record of the pool's resync unit, then cancels the scrub,
  which ends the resync; it does not stop the unit. When the stop or the
  cancel fails, the mark is taken back.
- **Store.** `/var/lib/linuxio/jobs/scheduled/` (`0700`, root) holds each
  run's `<id>.json` and `<id>.log` (`0600`). An output longer than 10 MiB keeps
  its first and last 5 MiB with one line between saying how many bytes were
  left out. Each unit keeps its newest 50 ended runs and none older than 30
  days, and a run's output goes with its record. The btrfs timer program and
  the Docker update runner write their run's progress to
  `/run/linuxio/jobs/scheduled/<id>.progress`, finding the run through
  `$INVOCATION_ID`.
- **Liveness.** The root job host lists the running scheduled runs for
  `tasks.list` and `tasks.events` ([API Contract](./api-contract.md#scheduled-runs)).
  At its start and at every listing it marks a `running` record `interrupted`:
  `reboot` when it is from an earlier boot, `crash` when systemd no longer runs
  its invocation: the unit is not loaded, runs another invocation, or is
  neither `activating`, `active`, `reloading` nor `deactivating`. Of the D-Bus
  errors only `NoSuchUnit` (a unit that is not loaded) counts as gone; any
  other leaves the record as it is.

`linuxio-btrfs-health.service`, the degraded mount at boot, the indexer's
timer and the distribution's package auto-update are not recorded.

## Boot & Login Flow

### Boot — nothing is running yet

```
linuxio.target → pulls in the webserver, auth, and indexer sockets
  systemd binds  TCP :8090            (webserver.socket)
  systemd binds  /run/linuxio/auth.sock (auth.socket, root:linuxio-bridge-socket 0660)
  systemd binds  /run/linuxio/indexer.sock (linuxio-indexer.socket, root:root 0600)
  No linuxio-webserver / linuxio-auth / linuxio-bridge / linuxio-indexer process exists.

multi-user.target → also pulls in, outside linuxio.target:
  linuxio-jobs@0.socket, then linuxio-jobs@0.service after the btrfs degraded mount:
    1. marks the root host's `running` job records interrupted: reboot, or crash for a host of this boot
    2. resumes the resumable root jobs the last shutdown or power loss interrupted (once; every other interrupted record loses `Resumable`)
    3. raises an alert for each other root job it marked, then exits once idle for 60 s
  A user's host marks their records the same way when the first connection after their login starts it.
```

### First request — webserver starts via socket activation

```
Browser → :8090
  systemd starts linuxio-webserver.service, passing the listening fd
  via LISTEN_FDS / LISTEN_PID.
  The service weakly starts linuxio-indexer.service and systemd creates
  /run/linuxio/webserver, which pins the indexer's idle grace while the
  webserver is alive. The webserver still cannot open the root-only socket.
  Webserver adopts it through the shared socket-activation helper in
  backend/common/socketactivation, called from backend/webserver/cmd/root.go.
  No bind race, no extra privilege.
```

### Login — auth instance forks and supervises a bridge

```
Webserver  ── connect ──►  /run/linuxio/auth.sock        (allowed via group membership)
                                  │  Accept=yes
                                  ▼
                    linuxio-auth@<conn>.service  (root)   ← connected socket is its stdin
                       1. PAM authenticates once; root queries sudoers with
                          `sudo -n -l -U <user> -u root -- linuxio-bridge`
                       2. fork → drop to user uid/gid (or stay root if privileged)
                       3. dup2 the SAME client socket onto bridge FD 3
                       4. pass bootstrap (session id, uid/gid, flags) via a pipe → bridge stdin
                       5. wait for exec confirmation, then reply OK to the webserver
                       6. waitpid(bridge) — BLOCKS for the whole session  ◄── supervises
                                  │
                                  ▼
                          linuxio-bridge  (user)  ── yamux server on FD 3
```

The auth instance does **not** exit after forking — it holds the PAM session open and blocks in `waitpid` as the bridge's parent for the entire login ([linuxio-auth.c](../backend/auth/linuxio-auth.c)). Consequences:

- A login has no task or memory cap. `pam_systemd` (optional in the PAM stack) moves the worker, and with it the bridge, into a logind session scope (`session-<n>.scope` under `user-<uid>.slice`), as it does an SSH login; without it they stay in the `linuxio-auth@.service` cgroup. The session holds the web terminal and the assistant's agents, whose work is the user's to size. Jobs are bounded by their host, `linuxio-jobs@.service` (`TasksMax=8192`, `MemoryMax=2G`).
- PAM session open/close brackets the bridge's lifetime exactly.
- One bridge per login is fully isolated from other logins, and each holds one of the socket's 64 connections (`MaxConnections=64`) until its session ends.

### Login and jobs — the bridge connects to its job host

```
linuxio-auth (root), once PAM authenticated an unprivileged login U, before the session opens and the bridge spawns:
          systemctl start --no-block linuxio-jobs@U.socket   (a listening socket: no-op; a failure is logged, the login proceeds)
linuxio-bridge running as uid U (0 for a privileged login), at startup, in the background:
  U = 0:  StartUnit linuxio-jobs@0.socket over the system bus itself
  then:   connect /run/linuxio/jobs/U.sock (0600, owned by U; systemd starts the host if it is down,
          and the host checks SO_PEERCRED = U), retrying for 5 s while the socket starts
          yamux; first stream jobs.hello {protocol, session {username, uid, gid, privileged, session_ref}}
          protocol mismatch → the host refuses the hello: an update waits for this older host
  failure or refusal → retry with backoff 1 s … 30 s; meanwhile mutations answer 503,
          or 409 update_pending after a refusal (see below)
```

**While an update waits.** A host still running the binary of an older build
refuses the hello of a newer bridge. Until it exits, the bridge cannot ask it
anything, so for `tasks.host` it reads that host's job store itself: the
records in `/var/lib/linuxio/jobs/<uid>/` and the progress files in
`/run/linuxio/jobs/<uid>/`, where `<uid>` is the bridge's own uid (0 for a
privileged login). The bridge runs as that uid, which owns the `0700`
directories, so the read needs no privilege of its own; it is read-only and
touches no other uid's store. It counts the `running` records of executor kind
`host` whose process still lives (pid and start time), lists them without
their paths and package IDs, and refuses with that count each new mutation it
would relay to the host (with none running the refusal says new jobs can
start shortly; with the store unreadable it names no count); the three
browser-bound mutations (`filebrowser.upload`, `filebrowser.upload_batch`,
`filebrowser.archive`) run in the bridge and are not affected.
`tasks.history` and `tasks.list` answer without the host's jobs, and
`tasks.get`, `tasks.watch`, `tasks.cancel` and `tasks.output`
of a host job answer `503` after waiting 10 s
([API Contract](./api-contract.md#ended-jobs-saved-output-and-updates)). A
refused hello does not count as a connected bridge, so the older host exits
60 s after its last job and its last bridge of its own build; the bridge's next
attempt, at most 30 s later, finds the socket free and systemd starts the new
binary.

From then on the bridge relays every Mutation stream over that connection and
serves `tasks.*` for host jobs (see [API Contract](./api-contract.md#tasks-and-jobs)).
At logout the bridge closes the connection; the host keeps running its jobs and
exits 60 s after its last bridge left and its last job ended. The next login of
the same uid reconnects to the same host if it is still up.

**needrestart.** On Ubuntu, needrestart, whose apt hook runs after every dpkg
run, restarts each service still running a replaced binary; for an older host
that interrupts its jobs (seen once in the test VM, during an unattended-upgrades
run; [jobs.md 3.9](./TODO/jobs.md), finding 2). Where `/etc/needrestart`
exists the installer therefore installs
`packaging/etc/needrestart/conf.d/linuxio.conf` as
`/etc/needrestart/conf.d/linuxio.conf`, after the release binaries are
verified and before it replaces any of them. Each of its lines adds one
regular expression with the value 0 ("do not restart") to needrestart's
`$nrconf{override_rc}` and keeps the distribution's entries:
`linuxio-jobs@<uid>.service`, the timer-run units `linuxio-schedule-*`,
`linuxio-btrfs-snapshot`, `linuxio-btrfs-resync@*` and
`linuxio-docker-update`, and the updater's `linuxio-updater-*` unit.
needrestart then lists them under "Service restarts being deferred" instead of
restarting them. The daemons the installer restarts itself (`linuxio-webserver`,
`linuxio-monitoring`, `linuxio-indexer`, `linuxio-auth`) are not listed, and
needrestart restarts them as before. The uninstaller removes the file.
needrestart evaluates the file as Perl, as root, and a release install fetches
it from `main` without a checksum; a file needrestart cannot load stops all its
restarts. So the installer installs it only when every line is blank, a
comment, or one such entry whose pattern has, token by token, the shipped form
(`^linuxio-`, lowercase words joined by `-`, at most one `@` or `-` followed
by `\d+` or `.+`, then `\.service$`), never for one of the four always-on
daemons, and, where Perl is present, only when the file compiles under
`strict` as needrestart loads it (`needrestart_override_valid`). Any other
file is not installed, an installed copy stays as it is, and the installer
prints a notice.

In the test VM, against needrestart 3.11-1ubuntu2 on Ubuntu 26.04
([jobs.md 3.9](./TODO/jobs.md), runs 4 and 5): in list mode needrestart put
`linuxio-jobs@0.service` under "Service restarts being deferred" and
`linuxio-monitoring.service` under "Services to be restarted"; in automatic
mode it deferred the job host, whose `MainPID` stayed, and restarted
`linuxio-monitoring.service`, which got a new one. That the other five
expressions match their units and no daemon, that the content check refuses
other files, and that the uninstaller removes the file is covered by the
installer's fixture tests, not seen in the VM.

An in-app update runs the installer in a `linuxio-updater-<n>.service` unit
with `ProtectSystem=full`, which keeps `/etc` read-only except for the unit's
writable paths; the bridge installed *before* the update starts that unit with
its own list (`updaterWritablePaths` in `backend/bridge/handlers/appupdate`).
The list has `/etc/needrestart/conf.d` as an optional entry (`-` in
`ReadWritePaths`: a system without it is unaffected, and the updater creates
nothing there), so the first in-app update launched by a build with that entry
installs the file (where `conf.d` exists, as Ubuntu's package ships it). An
update launched by an earlier build, which includes every release up to now,
cannot write it: the installer then prints a notice and the install goes on;
a run of the installer from a terminal, or the next in-app update, which the
newly installed bridge launches, installs it. That is read from the source and
the installer's fixture tests, which simulate a read-only directory; no in-app
update was run.

### After login — the connection becomes the yamux transport

The webserver keeps its end of the socket it dialed; it is now wired straight to the forked bridge (the auth daemon is out of the data path). The webserver wraps it as a yamux **client** and multiplexes WebSocket streams over it. From here on, see [Server Yamux Protocol](./server-yamux-protocol.md). When the bridge exits, the auth instance reaps it and closes the PAM session; the webserver's yamux session closes → the HTTP session is terminated. Jobs in the job host keep running.

## Privilege Boundaries (summary)

| Boundary | Mechanism | Guarantee |
|----------|-----------|-----------|
| Browser ↔ webserver | HTTP + session cookie, validated before WS upgrade | network-facing process is unprivileged + sandboxed |
| Webserver ↔ auth socket | unix socket, `root:linuxio-bridge-socket 0660` | only the webserver's group may request a login |
| Auth ↔ bridge | PAM authentication + root-side sudoers query + `fork`/privilege drop | bridge stays root only when sudoers permits the exact bridge command; otherwise it starts with the user's uid/gid |
| Webserver ↔ bridge | inherited socket fd + yamux | webserver never gains the bridge's privileges; just relays bytes |
| Auth ↔ job host socket | `linuxio-auth` runs `systemctl start` on the socket of the uid PAM admitted | a user's job host socket exists only after that user logged in through PAM, or when root starts it |
| Bridge ↔ job host | `/run/linuxio/jobs/<uid>.sock`, `0600` and owned by the uid, held by systemd + `SO_PEERCRED` uid check + hello protocol check | only that uid reaches or starts its host; the root host serves only root; hello session facts are owner metadata, never authorization |
| Bridge ↔ indexer socket | privileged route metadata + `root:root 0600` Unix socket | only a sudo-authorized root bridge reaches the machine-wide index |
| Bridge ↔ monitoring `api.sock` | `root:root 0666` Unix socket, read-only metrics API, no commands | any local process may read measurements it could already read from `/proc`; nothing can be mutated through it |
| Bridge ↔ monitoring `control.sock` | privileged route metadata + `root:root 0600` Unix socket + `SO_PEERCRED` uid-0 check | only a sudo-authorized root bridge reaches history, configuration and commands |
| Webserver → bridge launch | embedded SHA-256 pin (`version.BridgeSHA256`), checked by `validateBridgeHash` | a tampered/substituted bridge binary won't be spawned |

## Build & Install

The `Makefile` (recipes in `mk/go.mk` and `mk/auth.mk`) produces eight release artifacts into `build/` (`BIN_DIR` overrides it):

| Target | Output | Notes |
|--------|--------|-------|
| `make build-cli` | `linuxio` | Go |
| `make build-backend` | `linuxio-webserver` | Go; embeds version + **bridge SHA-256** |
| `make build-bridge` | `linuxio-bridge` | Go |
| `make build-auth` | `linuxio-auth` | C; hardened flags (RELRO, PIE, FORTIFY, stack-clash, LTO), links `libpam` + `libsystemd` |
| `make build-docker-update` | `linuxio-docker-update` | Go; scheduled Docker update worker |
| `make build-indexer` | `linuxio-indexer` | Go + SQLite; FTS5 enabled |
| `make build-monitoring` | `linuxio-monitoring` | Go + SQLite; `CGO_ENABLED=1`, plus the `glibc` tag on amd64 for NVML |
| `make build-btrfs-timer` | `linuxio-btrfs-timer` | Go; btrfs snapshot and health timer worker |

`make build-binaries` (used by `make build` and `make fastbuild`) builds all eight: `build-go-binaries` builds every Go binary, hashing the freshly built `linuxio-bridge` and passing it as `BRIDGE_SHA256` into the webserver build so the pin always matches, then `build-auth` builds the C helper. `make print-binaries` prints the shipped-binary manifest that the release workflow and installer tests read. Only `common/version.Version` is stamped into the binaries that report a version. Install via `make localinstall`, which runs `packaging/scripts/localinstall.sh` (a wrapper for `install-linuxio-binaries.sh --local --build-dir build/`):

- binaries → `/usr/local/bin/`
- units → `/etc/systemd/system/linuxio*`
- tmpfiles → `/usr/lib/tmpfiles.d/linuxio.conf`
- PAM stack → `/etc/pam.d/linuxio`
- config → `/etc/linuxio/`
- needrestart override → `/etc/needrestart/conf.d/linuxio.conf`, only where `/etc/needrestart` exists (see the **needrestart.** paragraph in "Login and jobs — the bridge connects to its job host")

## Managing the Stack (`linuxio` CLI)

```
linuxio status              # list all linuxio* units with colored state
linuxio logs [web|bridge|auth|indexer|monitoring] [N]   # tail journald, filtered per component
linuxio start | stop        # start/stop linuxio.target
linuxio restart [--full]    # restart control plane (bridge-socket-user + auth.socket + webserver);
                            #   --full restarts the whole linuxio.target
linuxio verbose enable|disable|status   # toggle debug logging for webserver, indexer and monitoring (SIGHUP reload; open sessions switch at next login); persists across restarts
linuxio version [--self]    # versions of CLI + each installed component
```

Settings drives the same switch through two privileged bridge routes:
`logging.get_debug` (none → `{ enabled }`) and `logging.set_debug`
(`{ enabled }` → `{ enabled }`). Both live in `backend/bridge/handlers/config`;
`set_debug` writes or removes the drop-ins (`backend/common/verbosemode`),
applies the level to that bridge and signals the monitoring and indexer daemons
and then the webserver (last, so a unit that restarts instead of reloading does
not tear the calling bridge down before the others are signalled). A job host
reads the drop-ins when it starts; one already running keeps its level until it
idles out. `set_debug` stays a Call rather than a Mutation: it is an instant,
idempotent switch whose point is the calling bridge's own level.

Neither `restart`, `stop` nor the installer stops a job host: they are outside
`linuxio.target`. `systemctl stop linuxio-jobs@<uid>` does, and marks that
host's running jobs `interrupted`. The next connection to its socket starts it
again, within 1–30 s while a bridge is connected, so stop
`linuxio-jobs@<uid>.socket` too, or mask the service, to keep one down.

`restart` (no args) cycles only the control-plane units — `linuxio-bridge-socket-user.service`, `linuxio-auth.socket`, `linuxio-webserver.service` — and deliberately leaves `linuxio-webserver.socket` alone, so the listening TCP fd on :8090 stays bound and browser connections aren't dropped (`restartTargets` in `backend/cli/services.go`). `--full` restarts the whole `linuxio.target`.

Journald, panic and `SIGQUIT` tracebacks, profiling builds, and core-dump limits
follow the [Production Diagnostic Data Policy](./production-diagnostics.md).

## File Locations

| Component | Path |
|-----------|------|
| Systemd units | `packaging/systemd/*.{target,socket,service}` |
| libvirt QEMU hook (bridged VM forwarding past Docker's FORWARD DROP) | `packaging/libvirt/hooks/qemu.d/linuxio-bridge-netfilter` → `/etc/libvirt/hooks/qemu.d/` |
| needrestart override (apt's needrestart run defers the job hosts and the timer-run units) | `packaging/etc/needrestart/conf.d/linuxio.conf` → `/etc/needrestart/conf.d/`, only where `/etc/needrestart` exists |
| Managed TLS certificate | `/var/lib/linuxio/webserver/certificates/0-self-signed.{cert,key}` |
| Install script | `packaging/scripts/install-linuxio-binaries.sh` (`--local` for a checkout); `packaging/scripts/localinstall.sh` is a wrapper for it |
| CLI (commands) | `backend/cli/` (`main.go` dispatch, `services.go`, `logs.go`, `verbose.go`, `version.go`) |
| Shared socket-activation adopt | `backend/common/socketactivation`, `backend/webserver/cmd/root.go` |
| Auth daemon (PAM, fork, supervise) | `backend/auth/linuxio-auth.c` |
| Bridge entry point | `backend/bridge/cmd/lifecycle.go`, `cmd/yamux.go` |
| Job host, client and their commands | `backend/bridge/internal/jobshost`, `backend/bridge/internal/jobsclient`, `backend/bridge/cmd/jobs_host.go` and `jobs_relay.go` (bridge → host relay) |
| Job store | `backend/common/jobs`; records in `/var/lib/linuxio/jobs/<uid>/`, live progress in `/run/linuxio/jobs/<uid>/`, the host socket at `/run/linuxio/jobs/<uid>.sock` |
| Scheduled runs | hooks in `packaging/systemd/linuxio-run-record.conf` (installed as `10-linuxio-run.conf` in the drop-in directories of the three LinuxIO units and the four distribution update services) and `linuxio-btrfs-resync@.service`; command `backend/cli/scheduled_run.go`; recording, output copy and liveness `backend/common/scheduledrun`; listing in the root host `backend/bridge/internal/jobshost/scheduled.go`; records, outputs and progress in `/var/lib/linuxio/jobs/scheduled/` and `/run/linuxio/jobs/scheduled/` |
| Indexer daemon and API | `backend/indexer/`, `backend/indexer/api/` |
| Monitoring daemon and shared wire types | `backend/monitoring/`, `backend/monitoring/api/` |
| Build | `Makefile` + `mk/go.mk` (`build-*`, `build-go-binaries`, `build-binaries`); output in `build/` |

## See Also

- [Server Yamux Protocol](./server-yamux-protocol.md) — what flows over the webserver↔bridge connection (byte relay + mux framing).
- [Monitoring Daemon](./monitoring.md) — the metrics collector, its two sockets, sampling semantics, and configuration.
- [Privilege Pattern](./privilege_pattern.md) — declaring privileged routes inside the bridge.
- [API Contract](./api-contract.md) — Go-owned API contract and generated frontend client.
- [Production Diagnostic Data Policy](./production-diagnostics.md) — credentials and safe correlation data across diagnostic sinks.
