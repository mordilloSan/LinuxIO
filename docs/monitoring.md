# Monitoring Daemon

LinuxIO ships `linuxio-monitoring` as a first-party, root-owned service. It
samples the host — CPU, memory, load, disk I/O, network, filesystem usage,
sensors, GPU, SMART and containers — serves those samples over two fixed Unix
sockets, and persists the history-enabled plugins in SQLite. Managed objects
that LinuxIO itself mutates (containers, mounts, services, users, shares,
network configuration) stay with `linuxio-bridge`; the daemon owns measurements,
not state.

The implementation derives from
[Beszel](https://github.com/henrygd/beszel) by way of the
[go-monitoring](https://github.com/mordilloSan/go-monitoring) fork, whose
pre-import history is kept in that repository. Attribution and dependency
licenses are recorded in [Third-Party Notices](THIRD_PARTY_NOTICES.md).

There is no migration from a standalone go-monitoring installation: the daemon
uses its own sockets, config path and database, and LinuxIO neither reads nor
removes an external install.

## Architecture and trust boundary

```text
Browser
  | authenticated WebSocket
linuxio-webserver (DynamicUser, relays request bytes)
  | per-session yamux connection
linuxio-bridge (enforces session and privileged-route policy)
  |                                        |
  | HTTP over world-readable Unix socket   | HTTP over root-only Unix socket
  v                                        v
/run/linuxio/monitoring/api.sock      /run/linuxio/monitoring/control.sock
  read-only metrics, any session        metrics plus commands, root peer only
              \                        /
               linuxio-monitoring (root collector, SQLite store)
```

The webserver does not parse bridge request payloads; it relays bytes between
the browser WebSocket and the authenticated session's yamux connection. The
bridge decides which socket a route may use: live measurements are read from
`api.sock` on any session, while history, status, configuration and restart are
privileged routes that reach `control.sock` only from a sudo-authorized root
bridge.

`api.sock` is mode `0666` on purpose. The metrics it serves are the same
counters any local process can read from `/proc` and `/sys`, so the socket
matches the loopback-TCP default the fork shipped with and the visibility of the
files it samples. `control.sock` is mode `0600` and additionally checks
`SO_PEERCRED` on every connection, rejecting any peer whose uid is not `0` with
`403 Forbidden` (`requireRootPeer` in
`backend/monitoring/internal/app/peer_gate.go`, over the
`backend/common/peercred` helper — the same uid gate the indexer applies on its
own socket). Commands are served only there; no configured listener ever mounts
`/api/v1/command`.

| Boundary | Owner |
|----------|-------|
| Collectors, delta trackers, live sampling | `linuxio-monitoring` |
| History persistence, aggregation, retention, maintenance | `linuxio-monitoring` SQLite store |
| Read-only metrics API and optional outside consumers | `linuxio-monitoring` listeners |
| Commands: config, status, SMART refresh, database checks | `linuxio-monitoring` control socket, root peers only |
| Live wire types and socket constants shared by daemon and bridge | `backend/monitoring/api` |
| Unix-socket clients and LinuxIO API adapters | `backend/bridge/handlers/monitoring` |
| Public generated bridge contract | `backend/bridge/apischema` |
| Hardware identity that needs a shutdown to change | `linuxio-bridge`, cached per session |
| Browser transport relay | `linuxio-webserver` |

Only `backend/monitoring/api` is importable outside the daemon; everything else
lives under `backend/monitoring/internal`.

## Installed files and units

| Path or unit | Purpose |
|--------------|---------|
| `/usr/local/bin/linuxio-monitoring` | Daemon binary, source `backend/monitoring/` |
| `/etc/linuxio/monitoring/config.yaml` | Strict YAML configuration, written by the daemon when absent |
| `/var/lib/linuxio/monitoring/metrics.db` | SQLite history store; `-wal` and `-shm` sidecars may appear beside it |
| `/run/linuxio/monitoring/api.sock` | Read-only metrics socket, mode `0666` |
| `/run/linuxio/monitoring/control.sock` | Metrics plus commands, mode `0600`, root peer only |
| `linuxio-monitoring.service` | Long-running collector, store and API owner |

The unit is `Type=simple`, runs as `root` (SMART, the Docker socket, hwmon and
the GPU collectors need host devices), is `PartOf=` and `WantedBy=linuxio.target`,
and restarts `on-failure`. `ExecReload` sends `SIGHUP`. systemd owns
`RuntimeDirectory=linuxio/monitoring`, `StateDirectory=linuxio/monitoring` and
`ConfigurationDirectory=linuxio/monitoring`; `ProtectSystem=strict` leaves
`/var/lib/linuxio/monitoring` and `/etc/linuxio/monitoring` as the only writable
paths. The daemon is not socket-activated and has no idle exit: history has to
be sampled whether or not a browser is connected.

The database path is fixed. There is no path setting and no command-line flag
for it; tests pass temporary directories straight to the internal store.

## Command line

```text
linuxio-monitoring run [--config PATH] [--verbose]
linuxio-monitoring --version
linuxio-monitoring --help
```

`run` is what the unit executes. There are no other subcommands: status,
configuration, history and database maintenance are reached through LinuxIO,
which sends them as commands on the control socket. Logging goes to journald
through `backend/common/logging` under the syslog identifier
`linuxio-monitoring`.

## Sampling semantics

Two paths share one delta mechanism keyed by a `uint16` interval key.

**Collector tick.** Every `collector.interval` (default `1m`) the daemon
collects every plugin under one key, hands the finished sample to live requests
that were waiting during the run, writes the history-enabled plugins to SQLite,
refreshes SMART when due, and runs retention and maintenance. History values are
averages over the collector interval and are stored at a single `1m` resolution.

**Live request.** Every current endpoint, including `/api/v1/live`, keeps its
own sample key and its own delta baseline. Two rules apply to every live key:

1. **Reuse within one second.** A request reuses its key's sample when the
   sample is under one second old, and joins the collection when one is already
   in flight; otherwise it collects. `captured_at` (or `captured_at_ms`) reports
   the sample's own time, not the request time. One dashboard polling several
   cards therefore causes at most one collection per second, and several
   dashboards share it. See `liveReuseWindow` in
   `backend/monitoring/internal/app/live_reuse.go`.
2. **A stale baseline reseeds from the collector.** When a live key's delta
   baseline is older than the last collector tick *and* that tick is itself at
   least one second old, that tick's counters replace the baseline before the
   delta is computed. A first request after an idle period averages over at most
   one collector interval instead of the whole idle time, and a key's first
   sample has a real window; the one-second condition (`reseedMinWindow` in
   `backend/monitoring/internal/app/reseed.go`, duplicated in the docker package
   because it cannot import `app`) keeps a request that arrived just after a
   tick off a millisecond-wide window, which would read as zero CPU or as a rate
   amplified by 1000/δ. Each manager carries its own reseed helper:
   `reseedCPUFromCollector` for cpu, `networkManager.reseedFromCollector` and
   `fsManager.reseedFromCollector` for network and disk, and
   `docker.Manager.ReseedFromCollector` for containers.

No live result is retained beyond the one-second window. A live request that
arrives while a collector pass is running waits on the app mutex; the
`/api/v1/{plugin}`, `/api/v1/all` and `/api/v1/system/summary` routes
additionally accept the finished collector sample through the handoff.

Sleeping disks stay asleep: filesystem usage is served from the disk-usage cache
when `collector.disk_usage_cache` is non-zero, and SMART data comes from the
SMART cache on its own refresh interval, refreshed with `smartctl -n standby` so
a standby drive keeps serving its cached record. Only counters and hwmon
temperatures are sampled per collection; the daemon reads those through
gopsutil's sysfs sensors (`backend/monitoring/internal/app/sensors.go`), not by
running a helper program.

## HTTP API

This is an in-repository service contract, not a network or third-party API.
Every route below is served on both fixed sockets; `/api/v1/command` is served
only on `control.sock`. Responses are JSON; errors use an HTTP status and a
JSON error body.

| Method and path | Purpose |
|-----------------|---------|
| `GET /healthz` | Collector liveness: `healthy`, `last_updated`, `age_seconds` |
| `GET /api/v1/meta` | Version, data directory, database path, live listeners, intervals, retention |
| `GET /api/v1/plugins` | Plugins mounted on this listener and their routes |
| `GET /api/v1/all` | Current sample for every mounted plugin |
| `GET /api/v1/{plugin}` | Current sample for one plugin |
| `GET /api/v1/{plugin}/history` | Stored history for one history-enabled plugin |
| `POST /api/v1/{plugin}/refresh` | Forces a refresh; only `smart` implements it, and only the control socket mounts it |
| `GET /api/v1/system/summary` | Cross-plugin summary; mounted only on unrestricted listeners |
| `GET /api/v1/live` | The LinuxIO-shaped live payload, see below |
| `POST /api/v1/command` | One command; `control.sock` only |

Plugin names are `cpu`, `mem`, `swap`, `load`, `diskio`, `fs`, `network`, `gpu`,
`sensors`, `containers`, `container_telemetry`, `processes`, `programs`,
`connections`, `irq` and `smart`. `processes` and `programs` are live-only and
never persisted.

`/healthz` answers from memory: it compares the last collector tick against the
configured interval and returns `503 Service Unavailable` only when no tick has
happened yet or the last one is older than twice the interval. A freshly started
daemon is healthy immediately, because `runtime.go` runs one `collectAndPersist`
before it opens the sockets. There is no health file.

### `GET /api/v1/live`

`api.Live` (`backend/monitoring/api/live.go`) is a byte-precise, LinuxIO-shaped
payload built for the bridge, which reads it over `api.sock`
(`monitoring.FetchLive`) and uses it today for Docker container metrics:

| Field | Content |
|-------|---------|
| `captured_at_ms` | Sample time in Unix milliseconds |
| `uptime_seconds` | Host uptime |
| `cpu` | Total percent, per-core percent, breakdown (user, system, iowait, steal, idle) and the three load averages |
| `memory` | Total, used, available, free, cached, buffers, shared, swap total, swap free, ZFS ARC and Docker-used bytes |
| `disks` | Per physical block device: read and write bytes per second and operations per second |
| `disk_io` | Host totals across those devices |
| `interfaces` | Per interface: rx and tx bytes per second plus rx and tx byte totals |
| `containers` | Own `captured_at_ms` and items with id, name, CPU percent (Docker's multi-core convention), memory bytes, rx and tx bytes per second, and block read and write bytes per second when container telemetry is fresh |

The `filesystems`, `sensors`, `gpus` and `smart` sections of the approved design
are not part of this payload yet, and the payload is not embedded in
`apischema`: the dashboard still reads those values through the bridge's own
`system.get_*` routes. Embedding `api.Live` in the public contract, exposing it
as `monitoring.get_live`, and moving those four sections into it come with the
later plans in [the plan document](TODO/linuxio-monitoring.md).

### Plugin allowlist on configured listeners

A configured listener may name the plugins it exposes. On such a listener only
the allowed plugin routes are mounted, `/api/v1/plugins` lists only those,
`/api/v1/all` returns only those, `/api/v1/system/summary` is not mounted at
all, and `/api/v1/live` zeroes every section whose plugins are not allowed.
Sections map to plugins as follows:

| Live section | Plugins |
|---|---|
| `cpu` | `cpu` |
| `memory` | `mem`, `swap` |
| `disks` and `disk_io` | `diskio` |
| `interfaces` | `network` |
| `containers` | `containers`, `container_telemetry` |

`captured_at_ms` and `uptime_seconds` are always present. The two fixed sockets
are unrestricted.

A configured listener whose address cannot be bound is logged and skipped: the
two fixed sockets still serve, so the address remains fixable over them, and the
`restart_required` flag stays set until it is.

### Commands

`POST /api/v1/command` takes `{"command": "...", "params": {...}, "request_id":
"..."}` and answers `{"ok": ..., "command": ..., "request_id": ...,
"restart_required": ..., "data": ...}`. A missing `request_id` is generated so
every command is correlatable in the journal.

| Command | Effect |
|---------|--------|
| `commands.list` | Lists the commands below |
| `status.get` | Runtime status: version, paths, live listeners, intervals, config source |
| `config.get` | The persisted configuration in its flat JSON form |
| `config.set` | Validates and saves the given fields, then live-applies them |
| `config.reload` | Re-reads the file and live-applies it, like `SIGHUP` |
| `smart.refresh` | Refreshes the SMART cache now |
| `db.check` | Integrity-checks the database |
| `db.maintain` | Runs database maintenance |

`config.set` reports `restart_required: true` whenever the request carries a
`listeners` key, identical or not, because listener changes only take effect on
a restart. `config.reload` reports it when the file's listeners differ from the
running ones.

## Configuration

`/etc/linuxio/monitoring/config.yaml` is strict YAML parsed with the
repository's `goccy/go-yaml` (`backend/monitoring/internal/config/config.go`).
Unknown keys, multiple documents and invalid values are rejected; an absent file
means defaults, which the daemon then writes. The defaults are:

```yaml
version: 1
collector:
  interval: 1m0s            # history sampling cadence
  smart_refresh_interval: 1h0m0s
  disk_usage_cache: 0s      # 0 re-reads usage on every collection; set e.g. 15m to keep sleeping disks asleep
history:
  retention: 720h0m0s
  plugins: [cpu, mem, swap, diskio, network, containers, container_telemetry]
  # intervals:              # per-plugin sampling interval, a whole multiple of collector.interval; absent means every tick
  #   containers: 5m0s
  #   container_telemetry: 5m0s
listeners: []               # empty means no TCP; the unix sockets are always on
# listeners:
#   - name: homepage
#     address: 0.0.0.0:45876
#     plugins: [cpu, mem, network]   # omitted or empty means all metrics plugins
```

| Field | Applies |
|-------|---------|
| `collector.interval` | Live, on `config.set` and `SIGHUP` |
| `collector.smart_refresh_interval` | Live |
| `collector.disk_usage_cache` | Live |
| `history.retention` | Live |
| `history.plugins` | Live; an empty list records nothing |
| `history.intervals` | Live; a plugin's row is written on every Nth collector tick |
| `listeners` | Needs a service restart |

`version` must be `1`. `collector.interval`,
`collector.smart_refresh_interval` and `history.retention` must be greater than
zero; `disk_usage_cache` may be `0s`, and no duration may be negative. Each
`history.intervals` entry names a history-capable plugin other than `smart`
(whose history follows `collector.smart_refresh_interval`) and must be a whole
multiple of `collector.interval`; the stored value is still the average over one
collector tick, sampled every Nth tick. Listener
names and addresses must be unique, `api` and `control` are reserved names, the
two fixed socket paths are reserved addresses, and every named plugin must
exist. An address may be
`host:port`, a bare port (which binds `127.0.0.1`), `unix:/path`, or an absolute
path; a configured unix socket is mode `0660`.

Configured listeners serve the read-only metrics API and are **unauthenticated**
— anything that can reach the address can read what the listener exposes, which
is why the plugin allowlist exists. They never serve commands. `SIGHUP`
(`systemctl reload linuxio-monitoring`) re-reads the file and applies the live
fields; a bad file leaves the running configuration untouched and logs the
error.

The fork's remaining knobs stay environment-only and are not exposed in the UI,
because they are host facts rather than timings: `NICS` (interface filter),
`SENSORS`, `SYS_SENSORS`, `PRIMARY_SENSOR` and `SENSORS_TIMEOUT`,
`GPU_COLLECTOR`, `SKIP_GPU` and `INTEL_GPU_DEVICE`, `SMART_DEVICES`,
`SMART_DEVICES_SEPARATOR` and `EXCLUDE_SMART`, `EXTRA_FILESYSTEMS` and
`FILESYSTEM`, `MEM_CALC`, `DOCKER_HOST`, `DOCKER_TIMEOUT` and
`EXCLUDE_CONTAINERS`, and `HTTP_LOG` for per-request logging. Set them with a
systemd drop-in on `linuxio-monitoring.service`. An invalid `DOCKER_HOST` or
`DOCKER_TIMEOUT` disables Docker monitoring with a logged error instead of
stopping the daemon, and `https://` Docker hosts are refused because the
client speaks plain HTTP. `allow_remote_commands` does not exist.

## Capability and failure behaviour

The `monitoring` capability ships with LinuxIO and is not installable from
Capability Manager. Detection is a `GET /healthz` on `api.sock`
(`checkMonitoringAvailability` in
`backend/bridge/handlers/system/capabilities.go`), so the capability reports
whether the daemon is running and has produced a sample, nothing about
packages.

`lm_sensors` and `smartmontools` remain ordinary bridge-detected, installable
capabilities, and the frontend keeps gating the sensor and SMART views on them.
The daemon itself needs neither for temperatures — it reads hwmon through
gopsutil — but it does shell out to `smartctl`, and when that binary is missing
it leaves the SMART data empty rather than failing a collection.

A daemon that is down does not break unrelated features: the bridge's live read
returns `ErrUnavailable`, the Docker container list still renders with its
per-container metrics marked `unavailable`, and the privileged history, status
and configuration routes surface the error to the UI. See
[Capabilities](capabilities.md).

## Troubleshooting

```bash
linuxio logs monitoring          # journald, filtered to the unit and identifier
systemctl status linuxio-monitoring.service --no-pager
ls -l /run/linuxio/monitoring/   # api.sock srw-rw-rw-, control.sock srw-------
```

Ask the daemon directly. The read socket needs no privileges:

```bash
curl --unix-socket /run/linuxio/monitoring/api.sock http://localhost/healthz
curl --unix-socket /run/linuxio/monitoring/api.sock http://localhost/api/v1/live
curl --unix-socket /run/linuxio/monitoring/api.sock http://localhost/api/v1/plugins
```

Commands need root, because the control socket checks the peer uid:

```bash
sudo curl --unix-socket /run/linuxio/monitoring/control.sock \
  -H 'Content-Type: application/json' \
  -d '{"command":"status.get"}' \
  http://localhost/api/v1/command
```

The same call without `sudo` is refused: the socket's `0600` mode already denies
the connection, and a peer that reaches the listener anyway is rejected by the
uid gate with `403 Forbidden`.

After editing the configuration file by hand:

```bash
sudo systemctl reload linuxio-monitoring    # SIGHUP: re-read and live-apply
sudo systemctl restart linuxio-monitoring   # needed for listener changes
```

`sudo linuxio verbose enable` turns on debug logging for the webserver, the
indexer and this daemon: it writes
`/etc/systemd/system/linuxio-monitoring.service.d/verbose.conf`, which re-runs
`ExecStart` with `--verbose`. `sudo linuxio verbose disable` removes the
drop-ins.

If the database has to be recovered manually, stop the service, move
`/var/lib/linuxio/monitoring/metrics.db*` aside, and start it again — the daemon
creates an empty store and begins recording from the next tick. Move rather than
delete, so the old history stays available for inspection.

## See Also

- [Process & Systemd Architecture](process-systemd-architecture.md) — where this
  unit sits among the LinuxIO processes and privilege boundaries.
- [Capabilities](capabilities.md) — detection, gating and the install flow.
- [Filesystem Indexer](indexer.md) — the other first-party daemon, same socket
  and privilege pattern.
- [linuxio-monitoring plan](TODO/linuxio-monitoring.md) — the approved design,
  including the parts still pending.
