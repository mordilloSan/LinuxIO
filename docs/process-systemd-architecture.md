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
| Operate/inspect the stack | `linuxio` (CLI) | Convenience wrapper over `systemctl`/`journalctl` → runs as the invoking admin. The only program an administrator types, and the only one with commands for people. |
| Run what systemd starts with a word | `linuxio-worker` | The timer jobs (btrfs snapshots, replication, health, resync, rebuild, the degraded mount at boot) and the unit hooks that record a scheduled run and raise alerts → **root**, started only by systemd, installed for root only. Every private command word lives here, so no other program takes one. |

## Binaries

| Binary | Lang | Source | Runs as | Lifetime | Role |
|--------|------|--------|---------|----------|------|
| `linuxio` | Go | `backend/cli/` | invoking user (often via `sudo`) | on-demand | CLI: status, logs, start/stop/restart, verbose, version |
| `linuxio-webserver` | Go | `backend/webserver/` | `DynamicUser` + group `linuxio-bridge-socket` | long-running (socket-activated) | HTTP + WebSocket relay; **yamux client** |
| `linuxio-auth` | C | `backend/auth/linuxio-auth.c` | `root` | per auth connection (supervises its bridge) | PAM authentication, sudoers policy query, fork + supervise bridge |
| `linuxio-bridge` | Go | `backend/bridge/` | logged-in user (root only if privileged); the job host as its uid | per login session; the job host per uid, started by a connection to its socket, idle exit | **yamux server**; executes reads and relays mutations to the job host; the job host runs mutations as jobs |
| `linuxio-docker-update` | Go | `backend/docker-update/` | root, sandboxed | per scheduled update run | Docker updater |
| `linuxio-indexer` | Go | `backend/indexer/` | root, sandboxed | socket activated; exits when idle | Filesystem scanner, SQLite index, local HTTP/SSE API |
| `linuxio-worker` | Go | `backend/worker/` | root | oneshot, started by systemd timers, the snapshot pass, the unit hooks and the auto-update drop-ins | Btrfs timeline/pre-update snapshots, replication, the periodic health check, the degraded mount at boot, resync and the automatic rebuild; the hooks that record scheduled runs and raise alerts |
| `linuxio-monitoring` | Go | `backend/monitoring/` | root, sandboxed | long-running (no idle exit) | Metrics collector, SQLite history, local HTTP API on two Unix sockets |
| `linuxio-claude-relay` | Go | `backend/claude-relay/` | logged-in user, never root | one per `claude` an Assistant chat starts | Runs the real `claude` for the ACP adapter and carries plugin pane requests to the bridge ([Assistant](./AI-assistant.md#plugin-panes)) |

`linuxio` installs to `/usr/bin/`, on `PATH`, and the other eight programs to
`/usr/libexec/linuxio/`, a folder root owns and only root may write, which is
not on `PATH`: systemd, `linuxio-auth` and LinuxIO's own programs start them
by their full path (`version.LibexecDir`). The CLI is a thin management
front-end; the remaining processes are independently constrained by their
responsibilities.

**Who may start each program.** `linuxio` is the only program meant to be
typed. The package installs the programs systemd runs as root (`linuxio-worker`,
`linuxio-docker-update`, `linuxio-auth`, `linuxio-indexer`,
`linuxio-monitoring`) at mode `0700` ([Packages](./packaging.md#what-is-installed-where)),
so another local user cannot run a private
word or flag of theirs, and the rest `0755` because they run as someone else:
`linuxio` as whoever types it, `linuxio-bridge` and `linuxio-claude-relay` as
the signed-in user, and `linuxio-webserver` as a dynamic user, whose ids
change and so can own no file. Those three take no command word and do
nothing by hand: the bridge and the relay stop without the connection their
parent hands them, and a webserver started by a user has that user's rights
and cannot open `auth.sock`. None of the programs is setuid.

**Versions.** The package installs all nine programs from one build, so they
have one version, `common/version.Version`. `linuxio version` (also
`linuxio --version`) prints its own, and the webserver's unauthenticated
`/api/version` answers `{"version": "..."}` with its own
(`backend/webserver/versionapi`); neither runs or reads about another
program, several of which are root only. The page polls `/api/version` only
to see that a restarted webserver answers again; the footer shows the
version the bridge's `control.version` reports.

Known limits: an administrator's own unit or override that calls
`linuxio alert ...`, starts a daemon with `run` or passes the indexer
`--config-file` (`--config` since v0.55) needs the current command line; the
package never rewrites an administrator's files.

### Binary invocation map

Only `linuxio` is an operator administration CLI, and it holds admin commands
only. One other program takes a command word, `linuxio-worker`, which systemd
alone starts; a job on a timer, a hook on a unit and an alert from a drop-in
all go through it. The remaining programs take flags and start with no word. A
new root action for a timer or a hook is a word of `linuxio-worker`, never a
command of `linuxio` and never a word on a daemon.

**One rule for the other eight.** Each answers `--help` (or `-h`) and
`--version`, spelled the same on every program, and nothing else on them is
meant for a person.

- `--version` prints one line, the program's name and its build
  ([Versions](#binaries)).
- `--help` says what the program is, what starts it and with which command
  line. It lists the flags systemd passes so that someone reading a unit file
  can tell what they mean, not so that they are typed.
- There is no `help` or `version` word, since a daemon starts with flags only,
  and no `-v`, which reads as `--verbose` beside a program that has that flag.
- Flags are written with two dashes, in units, drop-ins and help alike. Go's
  flag parser also takes one dash; nothing LinuxIO writes uses it.
- An argument a program does not know ends it with status 2, and the
  program does not start.

The Go programs answer through `version.Program`
(`backend/common/version/program.go`); `linuxio-auth` holds the same text in
C. A program installed `0700` cannot be started by another user at all, so
for that user the shell answers "Permission denied" to `--help` and
`--version` as to everything else. That is on purpose: the refusal comes from
the kernel before any code of the program runs, and `linuxio version` shows
anyone the version, which is that of all nine programs.

#### What each program accepts

| Program | Mode | What a person can type | What it really runs as |
|---|---|---|---|
| `linuxio` | open | `status`, `logs`, `start`, `stop`, `restart [--full]`, `verbose enable\|disable\|status`, `version` (also `--version`), `help` (also `-h`, `--help`) | Nothing else; it is the admin tool |
| `linuxio-webserver` | open | `--help`, `-h`, `--version` | The web server, started by `linuxio-webserver.service` with `[--port N] [--verbose]` |
| `linuxio-bridge` | open | `--help`, `-h`, `--version` | One process per signed-in user, started by `linuxio-auth` with no arguments, the inherited fd 3 and the auth bootstrap; also the job host, started by systemd (`linuxio-jobs@<uid>.service`) with no arguments and the `linuxio-jobs` socket |
| `linuxio-claude-relay` | open | `--help`, `-h`, `--version`, outside a chat only | Started only by `claude-agent-acp` in an Assistant chat, with Claude Code's own arguments and `LINUXIO_CLAUDE_BIN` naming the real CLI. While that variable is set every argument goes to Claude Code, those three too; without it, anything else exits 2 |
| `linuxio-auth` | root only | `--help`, `-h`, `--version` | The sign-in helper, started by systemd for each connection (`linuxio-auth@.service`) with no arguments |
| `linuxio-worker` | root only | `--help`, `-h`, `--version`; the help lists its words | The words systemd timers and hooks run (list below) |
| `linuxio-docker-update` | root only | `--help`, `-h`, `--version` | One Docker update pass, started by its timer with `[--config PATH]` |
| `linuxio-indexer` | root only | `--help`, `-h`, `--version` | The file index daemon, started by `linuxio-indexer.service` with `[--config PATH] [--verbose]`, and in a second role, `--index-mode`, the scanner that `linuxio-indexer-index.service` runs |
| `linuxio-monitoring` | root only | `--help`, `-h`, `--version` | The monitoring daemon, started by `linuxio-monitoring.service` with `[--config PATH] [--verbose]` |

"Open" is mode `0755` and "root only" is `0700`
([Who may start each program](#binaries)). `linuxio verbose` switches the
daemons live through a SIGHUP reload and open sessions at the next login; it
persists across restarts and is also in Settings.

#### Every flag

Seven programs start with flags only. These are all of them, beside `--help`,
`-h` and `--version`:

| Program | Flag | Default | Who passes it |
|---|---|---|---|
| `linuxio-webserver` | `--port N` | `8090` | No unit. systemd hands the server its listening socket; the port is for a start without one, and it is the port the redirect from HTTP to HTTPS names |
| `linuxio-webserver` | `--verbose` | off | The drop-in `linuxio verbose enable` writes |
| `linuxio-bridge` | none | | Started with no arguments |
| `linuxio-auth` | none | | Started with no arguments |
| `linuxio-claude-relay` | none of its own | | Every argument is Claude Code's |
| `linuxio-docker-update` | `--config PATH` | `/etc/linuxio/docker-update.json` | `linuxio-docker-update.service`, with the default path |
| `linuxio-indexer` | `--config PATH` | `/etc/linuxio/indexer/config.yaml` | `linuxio-indexer.service` and `linuxio-indexer-index.service`, with the default path |
| `linuxio-indexer` | `--verbose` | off | The drop-in `linuxio verbose enable` writes |
| `linuxio-indexer` | `--index-mode` | | `linuxio-indexer-index.service`, for the scanner: one full index, which takes its turn with the daemon over `/run/linuxio/indexer.sock` |
| `linuxio-monitoring` | `--config PATH` | `/etc/linuxio/monitoring/config.yaml` | `linuxio-monitoring.service`, with the default path |
| `linuxio-monitoring` | `--verbose` | off | The drop-in `linuxio verbose enable` writes |

`--index-mode` must come first on the command line, since it chooses what the
program does. The scanner reaches the daemon at a fixed path and takes no flag
for it: the daemon gets its listener from systemd and the bridge has the path
as a constant, so a flag could only point the scanner at nothing. The flags of the worker's words
are in the list below.

The words of `linuxio-worker` are:

- `snapshot [--now RFC3339]` and `snapshot pre-update`: the timeline
  snapshots of the btrfs timer, and the snapshot the pre-update hook takes.
- `replicate [--now RFC3339]`: the replication the btrfs timer runs.
- `health`, `degraded`, `resync <uuid>` and `rebuild <uuid>`: the other btrfs
  units.
- `scheduled-run begin|end --unit <unit>`: the hooks of the scheduled units
  ([Scheduled runs](#scheduled-runs)).
- `alert raise|resolve|auto-update|pending-updates`: the alert hooks
  ([Alerts](./alerts.md#hook-commands)).

The bridge and its job host look users up through `os/user`, which finds
SSSD and LDAP users only through cgo. The bridge is therefore always built
with `CGO_ENABLED=1` (`mk/go.mk`): on a host with no C compiler the build
fails, where Go's default would quietly produce a bridge that sees
`/etc/passwd` alone.

The private indexer modes are an implementation protocol between the daemon and
its systemd units, not alternate administration paths. Indexer status,
configuration, and indexing remain available through LinuxIO and its HTTP APIs.

## Systemd Units

Core units under `linuxio.target`:

```
linuxio.target                       umbrella; WantedBy=multi-user.target
├─ linuxio-webserver.socket          TCP :8090 (dual-stack) → activates webserver.service
│  └─ linuxio-webserver.service      runs `linuxio-webserver` (DynamicUser, sandboxed)
├─ linuxio-auth.socket               unix /run/linuxio/auth.sock (Accept=yes) → per-conn instance
│  └─ linuxio-auth@.service          one instance per connection; root; forks+supervises a bridge
├─ linuxio-indexer.socket            unix /run/linuxio/indexer.sock → activates linuxio-indexer.service
│  └─ linuxio-indexer.service        runs the managed daemon; exits when idle
├─ linuxio-indexer-index.timer      periodic start of linuxio-indexer-index.service
│  └─ linuxio-indexer-index.service the scanner (`linuxio-indexer --index-mode`): one full index, oneshot,
│                                    nice 19 and idle I/O; takes its turn with the daemon over its socket
├─ linuxio-btrfs-health.timer       every 10 min; enabled only on hosts with btrfs
│  └─ linuxio-btrfs-health.service  runs `linuxio-worker health`; root
├─ linuxio-monitoring.service        runs `linuxio-monitoring`; root; owns
│                                    /run/linuxio/monitoring/{api,control}.sock
├─ linuxio-bridge-socket-user.service  oneshot: materializes the linuxio-bridge-socket user/group
└─ linuxio-issue.service             oneshot: writes the login banner; its stop writes how to start LinuxIO
```

Outside `linuxio.target`, so restarting LinuxIO never cancels them:

```
linuxio-btrfs-degraded.service       boot oneshot, WantedBy=multi-user.target, enabled only on hosts
                                     with btrfs; mounts fstab pools with a missing disk degraded, and
                                     pools udev holds back for a device replace target normally;
                                     has udev evaluate again the disks of a complete fstab pool it
                                     left unready, and waits for systemd to mount it
                                     (`linuxio-worker degraded`)
linuxio-btrfs-resync@<uuid>.service  started by the health check once a returned disk is back;
                                     runs `linuxio-worker resync <uuid>` (scrub, then soft convert);
                                     its unit file carries the scheduled-run hooks itself
linuxio-btrfs-rebuild@<uuid>.service started by the health check for a missing member whose own
                                     disk came back, or whose hot spare is due; runs
                                     `linuxio-worker rebuild <uuid>` (erase the returned disk, device
                                     replace, check); carries the scheduled-run hooks itself
linuxio-jobs@<uid>.socket            /run/linuxio/jobs/<uid>.sock, 0600, owned by the uid; started by
                                     linuxio-auth at that user's login and by root for uid 0; a
                                     connection starts the host whenever it is down
└─ linuxio-jobs@<uid>.service        job host for one uid (`linuxio-bridge`, no arguments, User=<uid>);
                                     enabled for uid 0, so the root host starts at boot; exits when idle;
                                     TasksMax=8192, MemoryMax=2G; store in /var/lib/linuxio/jobs/<uid>,
                                     progress in /run/linuxio/jobs/<uid>
```

Key unit facts (see `packaging/systemd/`):

- **`linuxio-webserver.socket`** — `ListenStream=8090`, `BindIPv6Only=both` (one socket answers both the A and AAAA records Avahi publishes). systemd owns the listening fd; the service inherits it. `After=linuxio-auth.socket` makes a restart of `linuxio.target` stop this socket before the auth socket and start it after: with no ordering, systemd started `linuxio-auth.socket` in the same window, its `sd-chown` helper (a fork of PID 1) inherited the old :8090 fd and held it a few ms past PID 1's close, and the new bind failed with "Address already in use" (3 of 10 `linuxio restart --full` runs on Ubuntu 24.04, systemd 255; 0 of 50 with the ordering).
- **`linuxio-webserver.service`** — `ExecStart=/usr/libexec/linuxio/linuxio-webserver`, `DynamicUser=yes`, `Group=linuxio-bridge-socket`. `StateDirectory=linuxio/webserver` gives the dynamic user private persistent storage for the managed TLS certificate. `RuntimeDirectory=linuxio/webserver` is an indexer activity marker whose lifetime is owned by systemd; `Wants=linuxio-indexer.service` warms the indexer without making it a prerequisite. Extensive hardening: `ProtectSystem=strict`, `PrivateDevices`, `MemoryDenyWriteExecute`, `NoNewPrivileges`, `SystemCallFilter`, `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`, etc.
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
- **`linuxio-jobs@.service`** — template, instance = uid. `Type=notify` (READY once it serves, STOPPING on idle exit), `User=%i`, `ExecStart=/usr/libexec/linuxio/linuxio-bridge` with no arguments: the binary is the job host when systemd passes it the socket named `linuxio-jobs`, and refuses to run by hand without it. `Wants=` and `After=` its socket. `StateDirectory=linuxio/jobs/%i` and `RuntimeDirectory=linuxio/jobs/%i`, both `0700` and owned by the uid, hold the job store and live progress; `RuntimeDirectoryPreserve=yes`. `TimeoutStopSec=2min`, `KillMode=mixed`, `NoNewPrivileges=yes`, `TasksMax=8192` (room for 256 jobs each waiting on a subprocess tree) and `MemoryMax=2G` with `OOMPolicy=continue`, so an OOM kill of one job's subprocess fails that job instead of stopping the host. Ordered after local filesystems and `linuxio-btrfs-degraded.service`, and deliberately not `PartOf=linuxio.target`, so updates and `linuxio restart --full` never stop a running job. `WantedBy=multi-user.target`, enabled for uid 0 only, so the root host starts at boot. At every start a host marks the jobs its uid's last host left running as interrupted, `reboot` when that host ran in an earlier boot and `crash` otherwise; the root host then resumes the resumable jobs the last shutdown or a power loss interrupted (reason `shutdown` or `reboot`, no `cancel_requested`, an executor from an earlier boot), picks up, before it serves, those of a route that resumes after a crash or a stop by hand in this boot (`packages.update` after a crash, `storage.run_smart_test` after either), starts the resumes after a reboot once it serves (any other interrupted record loses `Resumable`), and alerts on the others. A host touches only its own uid's store. It accepts a connection only from its own uid (`SO_PEERCRED`) and a hello of its protocol number, stays up while a bridge is connected or a job runs, exits after 60 s with neither, and refuses new jobs above 256 running.
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
([Jobs](./jobs.md#scheduled-runs)): scheduled
scripts (`linuxio-schedule-<id>.service`), btrfs snapshots
(`linuxio-btrfs-snapshot.service`), btrfs replication
(`linuxio-btrfs-replicate.service`, which the snapshot pass starts), pool resyncs
(`linuxio-btrfs-resync@<uuid>.service`), pool rebuilds
(`linuxio-btrfs-rebuild@<uuid>.service`) and Docker updates
(`linuxio-docker-update.service`). Two hooks do it:

```ini
[Service]
ExecStartPre=-+/usr/libexec/linuxio/linuxio-worker scheduled-run begin --unit %n
ExecStopPost=-+/usr/libexec/linuxio/linuxio-worker scheduled-run end --unit %n
```

- **Where they are.** Never in a rendered unit: `schedules.run_now` refuses a
  schedule whose unit file differs from what the build renders, so the hooks
  live in packaged files. The package installs
  `packaging/systemd/linuxio-run-record.conf` as `10-linuxio-run.conf` in
  `/usr/lib/systemd/system/linuxio-schedule-.service.d/` (a prefix directory, so
  every `linuxio-schedule-<id>.service` gets it),
  `linuxio-btrfs-snapshot.service.d/`, `linuxio-btrfs-replicate.service.d/`
  and `linuxio-docker-update.service.d/`,
  and in the drop-in directories of the distributions' automatic update
  services (`apt-daily-upgrade.service.d/`, `dnf-automatic.service.d/`,
  `dnf5-automatic.service.d/`, `mintupdate-automation-upgrade.service.d/`),
  so a system update is recorded like LinuxIO's own timer runs; a host has
  one of those services at most, and systemd ignores a drop-in of a unit that
  does not exist. The drop-ins go with the package, and its removal deletes
  the alert drop-in the bridge writes beside them
  ([Removal](./packaging.md#removal)). `linuxio-btrfs-resync@.service` and
  `linuxio-btrfs-rebuild@.service` carry the two lines themselves. The
  snapshot, replication and Docker update services are package units, as
  the resync and rebuild templates are: a save writes only their timers,
  which hold a schedule and no command line
  ([Btrfs](./btrfs.md#replication), [Docker updates](./docker-updates.md#the-scheduled-pass)).
  Schedules saved before an update record their runs after it without being
  saved again.
- **`+` and `-`.** `+` runs the hook as root, outside the unit's `User=` and
  sandbox: a script runs as the user it was given, and the Docker update unit
  has `ProtectSystem=strict`. `-` makes systemd ignore a failed hook, a missing
  `linuxio` binary included, so recording never stops or fails a run. A hook
  reads nothing but `--unit` and the environment systemd gives it
  (`$INVOCATION_ID`, and for `end` `$SERVICE_RESULT`, `$EXIT_CODE` and
  `$EXIT_STATUS`), refuses a unit that is none of those above, and gives itself
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
  days, and a run's output goes with its record. The worker and
  the Docker updater write their run's progress to
  `/run/linuxio/jobs/scheduled/<id>.progress`, finding the run through
  `$INVOCATION_ID`. Both also write a summary of what the run did into the
  record's `result` before they exit, which the end hook keeps
  ([Jobs](./jobs.md#scheduled-runs)).
- **Liveness.** The root job host lists the running scheduled runs for
  `tasks.list` and `tasks.events` ([API Contract](./api-contract.md#scheduled-runs)).
  At its start and at every listing it marks a `running` record `interrupted`:
  `reboot` when it is from an earlier boot, `crash` when systemd no longer runs
  its invocation: the unit is not loaded, runs another invocation, or is
  neither `activating`, `active`, `reloading` nor `deactivating`. Of the D-Bus
  errors only `NoSuchUnit` (a unit that is not loaded) counts as gone; any
  other leaves the record as it is.

`linuxio-btrfs-health.service`, the degraded mount at boot and the indexer's
timer are not recorded.

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
    2. resumes the resumable root jobs the last shutdown or power loss interrupted (once; every other interrupted record loses `Resumable`; a host started again in the same boot picks up instead, before it serves, the jobs of a route that resumes after a crash or a stop, `packages.update` after a crash and `storage.run_smart_test` after either)
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
that interrupts its jobs ([Jobs](./jobs.md#updates)). The DEB ships an
override that keeps it from restarting the job hosts and the timer-run units
([The needrestart override](./packaging.md#the-needrestart-override)).

### After login — the connection becomes the yamux transport

The webserver keeps its end of the socket it dialed; it is now wired straight to the forked bridge (the auth daemon is out of the data path). The webserver wraps it as a yamux **client** and multiplexes WebSocket streams over it. From here on, see [Server Yamux Protocol](./server-yamux-protocol.md). When the bridge exits, the auth instance reaps it and closes the PAM session; the webserver's yamux session closes → the HTTP session is terminated. Jobs in the job host keep running.

## Privilege Boundaries (summary)

| Boundary | Mechanism | Guarantee |
|----------|-----------|-----------|
| Browser ↔ webserver | HTTP + session cookie, validated before WS upgrade | network-facing process is unprivileged + sandboxed |
| Browser ↔ page scripts | `Content-Security-Policy` with `script-src 'self'` plus a SHA-256 of each inline script in the embedded `index.html`, computed at startup (`contentSecurityPolicy`); also `object-src 'none'`, `base-uri 'self'`, `frame-ancestors 'none'` | a script injected into the page does not run; only bundled files and the page's own boot script do. The Vite development server serves its own page and does not send this header |
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
| `make build-bridge` | `linuxio-bridge` | Go; `CGO_ENABLED=1`, so `os/user` finds SSSD and LDAP users |
| `make build-auth` | `linuxio-auth` | C; hardened flags (RELRO, PIE, FORTIFY, stack-clash, LTO), links `libpam` + `libsystemd` |
| `make build-docker-update` | `linuxio-docker-update` | Go; scheduled Docker updater |
| `make build-indexer` | `linuxio-indexer` | Go + SQLite; FTS5 enabled, extension loading left out |
| `make build-monitoring` | `linuxio-monitoring` | Go + SQLite; `CGO_ENABLED=1`, plus the `glibc` tag on amd64 for NVML |
| `make build-worker` | `linuxio-worker` | Go; the timer jobs and unit hooks systemd starts with a word |

`make build-binaries` (used by `make build` and `make fastbuild`) builds all eight: `build-go-binaries` builds every Go binary, hashing the freshly built `linuxio-bridge` and passing it as `BRIDGE_SHA256` into the webserver build so the pin always matches, then `build-auth` builds the C helper, and `third-party-notices` writes the notices of what the programs hold beside them ([Packages](./packaging.md#third-party-notices)). `make print-binaries` prints the shipped-binary manifest that the release workflow and the package tests read. Only `common/version.Version` is stamped into the binaries that report a version. `make localinstall` assembles the DEB or the RPM for the host's family from them and installs it with `apt` or `dnf` ([Development](./development.md)). Where the package puts each file, and what its install, upgrade and removal steps do, is in [Packages, installation and updates](./packaging.md).

### Login banner

`linuxio-issue.service`, which `linuxio-webserver.socket` pulls in, runs
`/usr/share/linuxio/issue/update-issue` once. It writes the addresses
LinuxIO answers on into `/run/linuxio/issue`: the host name and the source
address of the default route (with no default route, the first global IPv4
address on an interface that is not `lo`, `docker*`, `virbr*` or `br-*`), on
the port of the last line of `systemctl show --property Listen
linuxio-webserver.socket`, so a port set in a drop-in is the port shown. The
service is `PartOf=linuxio.target`, and its stop (`update-issue stop`) writes
"Start LinuxIO with: linuxio start" into the same file, so a stopped LinuxIO
advertises no address that does not answer. The design is Cockpit's
(`src/systemd/update-issue`), with one file rather than an active and an
inactive file and a link switched between them: the SELinux module names
`/run/linuxio/issue`, and `update-issue` relabels it after each write.

Links to `../../run/linuxio/issue`, which the package makes, show the file
on both families: `/etc/motd.d/linuxio` at an SSH login and
`/etc/issue.d/linuxio.issue` on the console
([The login banner](./packaging.md#the-login-banner)). On the RHEL family
the SELinux module lets the console's programs read the file
([SELinux](#selinux)).

## SELinux

On the RHEL family SELinux enforces by default, and without a policy of its
own nobody could sign in: systemd ran `/usr/libexec/linuxio/linuxio-auth` (`bin_t`)
as `unconfined_service_t`, the domain of a service the policy knows nothing
about; PAM authenticated, `pam_selinux.so open` set the context the user's
programs start in, `unconfined_u:unconfined_r:unconfined_t:s0`, and the
policy refused the exec of the bridge in it (`avc: denied { transition }`
from `unconfined_service_t` to `unconfined_t`). Only a login program's domain
(`sshd_t`, `cockpit_session_t`) may make that switch. So LinuxIO ships an
SELinux policy module, `linuxio` (`packaging/selinux/linuxio.cil`), as Cockpit
does for `cockpit-session`.

- **`linuxio_auth_exec_t`** is the type of `/usr/libexec/linuxio/linuxio-auth`, and
  **`linuxio_auth_t`** the domain systemd enters when it runs that file.
- **`linuxio_auth_t` is a `daemon`.** systemd creates the listening socket of
  `linuxio-auth.socket` in the domain of the program the service runs, and
  the policy lets `init_t` create sockets of daemons only: without the
  attribute the socket fails to listen ("Permission denied").
- **It is as unconfined as `unconfined_service_t`**, whose attributes it
  carries, and a `daemon` with the type transition every daemon domain has:
  `linuxio-auth` runs as root and was unconfined before; the module does not
  set out to confine it.
- **One permission more:** to start the bridge as `unconfined_t`. Measured
  on Fedora 45 with setools, attributes expanded and conditional rules taken
  with the booleans as set: the only permissions `linuxio_auth_t` has that
  `unconfined_service_t` lacks are `entrypoint` on its own file and that
  transition. It lacks about a thousand (type, class) pairs of
  `unconfined_service_t`'s (the X server, PostgreSQL, `nscd` and `passwd`
  classes, `execmem`, `execstack`, and transitions to `chronyc_t`,
  `passwd_t`, `rpm_script_t` and `svirt_t`). What other domains may do to it
  and not to `unconfined_service_t`, through `daemon` and the type
  transition: `nscd_t` reads its `/proc` entries; the `initrc_domain`
  members (`initrc_t`, `cluster_t`, `condor_startd_t`, `glusterd_t`,
  `kdumpctl_t`, `openshift_initrc_t`) enter it when they run `linuxio-auth`;
  user domains get the labelled-networking `recvfrom` on its UDP sockets.
  `staff_t` has no `connectto` on `unconfined_service_t` sockets, the job
  hosts' included.
- **A constraint keeps confined users out.** No line of the distribution's
  login contexts (`/etc/selinux/targeted/contexts/default_contexts` and
  `contexts/users/*`) names `linuxio_auth_t`, so `pam_selinux` falls back to
  `failsafe_context`, `unconfined_r:unconfined_t`, with the user's SELinux
  user. `staff_u` may take that role, so without more a `staff_u` user would
  get an unconfined bridge. The constraint allows the switch out of
  `linuxio_auth_t` only into the SELinux user `unconfined_u` (or the same
  user, as for programs `linuxio-auth` runs before the session).
- **The login banner.** The module also gives `/run/linuxio/issue`, which
  `/etc/motd.d/linuxio` links to, the type of `/run/motd.d`,
  `motd_var_run_t`: in its folder's type, `var_run_t`, `pam_motd` in `sshd`
  may not read it, and the banner never showed on Fedora. `update-issue`
  relabels the file after each write.
- **Two permissions for domains that are not LinuxIO's**, so the console
  shows the banner through `/etc/issue.d/linuxio.issue`: `getty_t`
  (`agetty`, the serial console) and `kmscon_t` (`kmscon`, Fedora's console
  on the screen, which reads `/etc/issue.d` itself) get `getattr`, `open`
  and `read` on files (class `file`) of type `motd_var_run_t`. The policy
  gives `login_pgm` (`sshd`, `login`) that read and neither of the two; with
  `var_run_t` `agetty` could read the file and `sshd` could not. They may
  now read every file of that type, which on Fedora 45 are
  `/run/linuxio/issue`, `/run/motd` and what is in `/run/motd.d`: messages
  shown at every login, and `getty_t` already reads every `var_run_t` file.
  Each rule is in an `optional` block of its own (`linuxio_banner` with the
  file context, `linuxio_banner_kmscon`), so a policy without `kmscon_t`
  drops only that rule. Measured on Fedora 45 in permissive mode: those
  three permissions are all either program asks for.
  `package-files-fixtures.sh` holds the module's rules for other domains to
  these two lines.
- **The swap file.** The memory page's swap file,
  `/var/lib/linuxio/swap/swapfile`, and the file a resize turns on beside
  it, `swapfile.linuxio-resize`, have the file context `swapfile_t`, in the
  `optional` block `linuxio_swapfile`. `mkswap` gives a regular file that
  type itself, so the file has it once it holds a swap header; the context
  keeps `restorecon`, `fixfiles` or a full relabel from turning it into
  `var_lib_t`, the type of its folder. Neither type matters for turning the
  swap on: `fsadm_t`, the domain `swapon` runs in at boot, is in
  `files_unconfined_type`, and on Fedora 45 the file was turned on from
  `/etc/fstab` at boot, with no denial, labelled either way. What the type
  decides is who may read the memory pages the file holds: measured as the
  module was measured (attributes expanded, booleans as set, unconfined
  domains left out), 23 confined domains may open and read a `var_lib_t`
  file and not a `swapfile_t` one (`httpd_t`, `cupsd_t`, `dovecot_t`,
  `init_t`, `user_t`, `staff_t` among them), and only `systemd_logind_t`
  and `systemd_sleep_t`, for hibernation, the other way round. The block
  holds the two file contexts and nothing else: it declares no type and
  gives no domain, `linuxio_auth_t` included, a permission; the policy
  `semodule` builds with it has the same rules as without it, and only
  `file_contexts` gains the two lines. `linuxio_auth_t` already has every
  permission on every file type, as `unconfined_service_t` has, and another
  local user's domain gains nothing either, since `swapfile_t` is the
  narrower type. The resize file is in it because it holds swapped-out
  pages too, and a resize that fails can leave it behind
  ([Memory](./memory.md)). The folder, `/var/lib/linuxio/swap`, keeps
  `var_lib_t`. A btrfs subvolume is made without a label
  (`unlabeled_t`, shown as `?` by `ls -Z`), so on btrfs LinuxIO gives the
  subvolume it makes the context of `/var/lib/linuxio` (`labelLikeParent`);
  where that cannot be set the folder stays `unlabeled_t`, which the boot
  was the same with, and the file is labelled by `mkswap` either way.
  `package-files-fixtures.sh` holds the block to these two lines.
- **Finding a denial.** `agetty`'s refusal was logged
  (`avc: denied { read } ... comm="agetty" ... tcontext=...motd_var_run_t`)
  in `/var/log/audit/audit.log` and the journal. `ausearch` run over a
  non-interactive SSH command reads its standard input instead of the log
  and prints `<no matches>`; give it `--input-logs`.

What runs where, seen on Fedora 45 with SELinux enforcing after a fresh
install and a reboot:

| Process or file | Context |
|---|---|
| The listening socket of `linuxio-auth.socket`, the connections it accepts, `linuxio-auth` | `system_u:system_r:linuxio_auth_t:s0` |
| The bridge of a sudo user (uid 0) and of a user without sudo | `unconfined_u:unconfined_r:unconfined_t:s0` |
| Job hosts, web server, monitoring, indexer | `system_u:system_r:unconfined_service_t:s0`, as without the module |
| `/usr/libexec/linuxio/linuxio-auth` | `linuxio_auth_exec_t` |
| `/run/linuxio/issue` | `motd_var_run_t` |
| `/var/lib/linuxio/swap/swapfile` (and `swapfile.linuxio-resize` while a resize runs) | `swapfile_t` |

Who gets in:

| User mapped to | Result |
|---|---|
| `unconfined_u` (the default mapping) | Signs in |
| `staff_u` | Refused when the bridge starts, by the constraint (`avc: denied { transition }` to `staff_u:unconfined_r:unconfined_t`); the login answers 500 `bridge_error`, and the page says it could not start the session bridge |
| `user_u` | Refused when the session opens: `pam_selinux` reports "Unable to get valid context", `user_u` having no `unconfined_r`; the same answer |
| Any, with a wrong password or listed in `/etc/linuxio/disallowed-users` | Refused by PAM (401), as without the module |

**Why hand-written CIL.** Compiling a `.te` module on the host at install
time needs `selinux-policy-devel` and what it pulls in (3 MiB to download,
26 MiB installed). The CIL converted from such a module is 259 lines
nobody reviews. Neither can carry the constraint: `checkmodule` refuses
`constrain` in a module, and CIL takes it. The hand-written file is about 100
lines and needs no build step and nothing beyond `semodule` and
`restorecon`, which a host with an SELinux policy has. Only the banner's file
context, the console's two reads and the swap files' contexts are in
`optional` blocks, so a policy that lacks one of the other names it uses
refuses the whole module.

**Why confined users are refused, not given a confined session.** A session
in the user's own domain (`user_t`, `staff_t`) was tried. It needs lines added
by hand to the distribution's login-context files, which an update of the
policy package may replace (`default_contexts` is replaced, `users/*` are
kept), and a permission for user domains to use `linuxio-auth`'s sockets.
Even then it is half broken: `user_t` may not connect to
`/run/linuxio/jobs/<uid>.sock` or to the monitoring socket, so there are no
jobs, no changes and no monitoring, and a listing of a folder holding a file
the domain may not stat (`/etc/audit`) fails whole. And a job host, which
systemd starts, runs as `unconfined_service_t` whoever it works for, so
opening its socket to a confined user would run that user's jobs unconfined.

**Install and removal.** The RPM carries the module and loads it in its
post-install step, then relabels `/usr/libexec/linuxio/linuxio-auth`, before
any unit is enabled or started, and an upgrade loads the new package's module
the same way; an erase removes the module after the files, since removed
while `linuxio-auth` is still there it would leave that file `unlabeled_t`,
which systemd cannot run (status `203/EXEC`)
([The SELinux module](./packaging.md#the-selinux-module)). A module that does
not load does not stop the install: without it sign-in is refused where
SELinux enforces. On a host with SELinux disabled `semodule` works on the
store and `restorecon` does nothing; switching SELinux on asks for a relabel,
which gives the binary its type. The Debian family is left alone:
`unconfined_service_t` is a name of Fedora's policy. The banner's file
context and the `getty_t` read sit in an optional block, so a policy without
`motd_var_run_t` loads the rest and only the banner keeps `var_run_t`; the
`kmscon_t` read sits in one of its own.

**Turned down, likely to be proposed again** (one line each under Rejected
in the [backlog](./TODO/backlog.md#rejected)): dropping `pam_selinux.so open`
from the PAM file works, but every session then runs as
`unconfined_service_t`, a confined user's too. `SELinuxContext=` on
`linuxio-auth@.service` worked as a drop-in on a running system and fails as
installed: systemd creates the listening socket in that context, and the
policy refuses `init_t` an `unconfined_t` socket.

### Known limits

- A user mapped to a confined SELinux user (`user_u`, `staff_u`) cannot sign
  in, so a host that confines every login has no LinuxIO sign-in, and the
  page shows only the general "could not start the session bridge" message.
- Job hosts, the web server, monitoring and the indexer stay
  `unconfined_service_t`.
- The bridge's level is `s0` where an SSH login has `s0-s0:c0.c1023`:
  `pam_selinux` takes it from `failsafe_context`.
- A user SELinux refuses (`user_u` when the session opens, `staff_u` when
  the bridge starts) still has their `linuxio-jobs@<uid>.socket` started:
  `linuxio-auth` starts it before the session opens.
- The file context names `/usr/libexec/linuxio/linuxio-auth`, where the package installs it.
- A policy that lacks a name the module uses (a strict or MLS policy, or one
  with `unconfined` removed) does not load it, and sign-in is refused there
  as before.
- A Debian-family host with SELinux enforcing is not handled: no module is
  installed there.
- The banner's label and the console's reads were checked on Fedora 45
  only; the screen console's reading by `kmscon` was seen with `strace` on a
  VM with no display, not on a screen. A policy whose file-context
  substitutions map `/run` to `/var/run` may not match `/run/linuxio/issue`,
  and the banner then stays unreadable to `sshd`.
- Checked on Fedora 45 only. That RHEL, CentOS Stream, Rocky Linux and
  AlmaLinux have every name the module uses is expected, not seen; an update
  of the policy package to a newer version was not seen either (a reinstall
  of the same version kept the module and the labels).

## Managing the Stack (`linuxio` CLI)

```
linuxio status              # list all linuxio* units with colored state
linuxio logs [web|bridge|auth|indexer|monitoring] [N]   # tail journald, filtered per component
linuxio start | stop        # start/stop linuxio.target
linuxio restart [--full]    # restart control plane (bridge-socket-user + auth.socket + webserver);
                            #   --full restarts the whole linuxio.target
linuxio verbose enable|disable|status   # toggle debug logging for webserver, indexer and monitoring (SIGHUP reload; open sessions switch at next login); persists across restarts
linuxio version             # the version of the build, which is the package's; also linuxio --version
```

With no command, or with `help`, `-h` or `--help`, it prints the list of
commands. `man linuxio` describes each command, which need root, the files
and the units (`packaging/man/linuxio.8`).

The Services page warns before it cuts LinuxIO off. Stop, Restart, Disable and
Mask on `linuxio.target` or a `linuxio-*` unit ask for confirmation first
(Start, Enable, Unmask and Reload do not), saying that this can end the session
or stop part of LinuxIO until `systemctl start linuxio.target` is run from a
shell. The network interface card does the same before disabling a connection
or applying IP settings on an interface the browser may be reaching LinuxIO
through: one that is connected with an IPv4 address and whose address matches
`window.location.hostname`, or, when the hostname is a name or an IPv6 literal
the interface list cannot be matched against, any such interface. A loopback
hostname never warns. Both warn and then allow; nothing is refused.

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

Neither `restart`, `stop` nor a package upgrade stops a job host: they are outside
`linuxio.target`. `systemctl stop linuxio-jobs@<uid>` does, and marks that
host's running jobs `interrupted`. The next connection to its socket starts it
again, within 1–30 s while a bridge is connected, so stop
`linuxio-jobs@<uid>.socket` too, or mask the service, to keep one down.

`restart` (no args) cycles only the control-plane units — `linuxio-bridge-socket-user.service`, `linuxio-auth.socket`, `linuxio-webserver.service` — and deliberately leaves `linuxio-webserver.socket` alone, so the listening TCP fd on :8090 stays bound and browser connections aren't dropped (`restartTargets` in `backend/cli/services.go`). `--full` restarts the whole `linuxio.target` and waits for `linuxio-webserver.socket` to listen again (`awaitWebSocket`). The `After=linuxio-auth.socket` ordering keeps LinuxIO's own helper off the old listener, but a helper any other unit forks in that window, such as a user's job host socket at a login, can still hold it for a moment, and the bind then fails with "Address already in use". systemd ends that start with the socket's result `resources` (the errno reaches only the journal), so on that result the CLI starts the socket again, up to three times half a second apart, and reports the result if the last start fails too; any other result is reported at once. Starting the socket again also starts `linuxio-indexer.socket`, which it wants and whose start failed with it, and `linuxio-webserver.service` starts on the next connection, as at boot.

Known limits: a listener held for longer than the retries (about two seconds in all), or another program listening on :8090, still fails `restart --full`; `linuxio start` binds the socket once the port is free. The result `resources` also covers the socket's other failures to open its listener, such as running out of file descriptors, and those get the same three retries.

Journald, panic and `SIGQUIT` tracebacks, profiling builds, and core-dump limits
follow the [Production Diagnostic Data Policy](./production-diagnostics.md).

## File Locations

| Component | Path |
|-----------|------|
| Systemd units | `packaging/systemd/*.{target,socket,service}` |
| libvirt QEMU hook (bridged VM forwarding past Docker's FORWARD DROP) | `packaging/libvirt/hooks/qemu.d/linuxio-bridge-netfilter` → `/etc/libvirt/hooks/qemu.d/` |
| needrestart override (apt's needrestart run defers the job hosts and the timer-run units) | `packaging/etc/needrestart/conf.d/linuxio.conf` → `/etc/needrestart/conf.d/`, DEB only |
| Managed TLS certificate | `/var/lib/linuxio/webserver/certificates/0-self-signed.{cert,key}` |
| Packages | `packaging/debian`, `packaging/rpm/linuxio.spec`, the shared tree `packaging/scripts/package-root.sh` ([Packages](./packaging.md)) |
| SELinux module (RHEL family) | `packaging/selinux/linuxio.cil` → `/usr/share/selinux/packages/targeted/`, loaded into the policy store by the RPM's `%post` and removed by its `%postun`; `packaging/scripts/update-issue` relabels the banner |
| CLI (commands) | `backend/cli/` (`main.go` dispatch and help, `services.go`, `logs.go`, `verbose.go`, `version.go`) |
| CLI man page | `packaging/man/linuxio.8`, handwritten roff → `/usr/share/man/man8/linuxio.8.gz` by both packages (`debian/linuxio.manpages`, the RPM's `%install`) |
| Worker (timer jobs and unit hooks) | `backend/worker/` (`main.go` dispatch; `snapshot.go`, `replicate.go`, `health.go`, `degraded.go`; `scheduled_run.go`, `alert.go`) |
| Program folders and modes | `packaging/scripts/package-root.sh` |
| Shared socket-activation adopt | `backend/common/socketactivation`, `backend/webserver/cmd/root.go` |
| Auth daemon (PAM, fork, supervise) | `backend/auth/linuxio-auth.c` |
| Bridge entry point | `backend/bridge/cmd/lifecycle.go`, `cmd/yamux.go` |
| Job host, client and their commands | `backend/bridge/internal/jobshost`, `backend/bridge/internal/jobsclient`, `backend/bridge/cmd/jobs_host.go` and `jobs_relay.go` (bridge → host relay) |
| Job store | `backend/common/jobs`; records in `/var/lib/linuxio/jobs/<uid>/`, live progress in `/run/linuxio/jobs/<uid>/`, the host socket at `/run/linuxio/jobs/<uid>.sock` |
| Scheduled runs | hooks in `packaging/systemd/linuxio-run-record.conf` (installed as `10-linuxio-run.conf` in the drop-in directories of the four LinuxIO units and the four distribution update services), `linuxio-btrfs-resync@.service` and `linuxio-btrfs-rebuild@.service`; command `backend/worker/scheduled_run.go`; recording, output copy and liveness `backend/common/scheduledrun`; listing in the root host `backend/bridge/internal/jobshost/scheduled.go`; records, outputs and progress in `/var/lib/linuxio/jobs/scheduled/` and `/run/linuxio/jobs/scheduled/` |
| Indexer daemon and API | `backend/indexer/`, `backend/indexer/api/` |
| Monitoring daemon and shared wire types | `backend/monitoring/`, `backend/monitoring/api/` |
| Build | `Makefile` + `mk/go.mk` (`build-*`, `build-go-binaries`, `build-binaries`); output in `build/` |

## See Also

- [Server Yamux Protocol](./server-yamux-protocol.md) — what flows over the webserver↔bridge connection (byte relay + mux framing).
- [Monitoring Daemon](./monitoring.md) — the metrics collector, its two sockets, sampling semantics, and configuration.
- [Privilege Pattern](./privilege_pattern.md) — declaring privileged routes inside the bridge.
- [API Contract](./api-contract.md) — Go-owned API contract and generated frontend client.
- [Production Diagnostic Data Policy](./production-diagnostics.md) — credentials and safe correlation data across diagnostic sinks.
