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
pre-import history is kept in that repository. The attribution is recorded
in [Third-Party Notices](THIRD_PARTY_NOTICES.md), and the licenses of the
dependencies in the notices a package installs
([Packages](./packaging.md#third-party-notices)).

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
  live metrics, any session             metrics, history and commands,
                                        root peer only
              \                        /
               linuxio-monitoring (root collector, SQLite store)
```

The webserver does not parse bridge request payloads; it relays bytes between
the browser WebSocket and the authenticated session's yamux connection. The
bridge decides which socket a route may use: live measurements are read from
`api.sock` on any session, while history, status, configuration and restart are
privileged routes that reach `control.sock` only from a sudo-authorized root
bridge.

`api.sock` is mode `0666` on purpose, so a session without sudo can show the
dashboard. Most of what it serves are the counters any local process can read
from `/proc` and `/sys`, but not all: it also serves the SMART records (smartctl
runs as root), the container list (the Docker socket is root's), and every
process's command line and disk I/O counters (`/proc/<pid>/io` is readable by
its owner only). It serves no history: the history routes are mounted on
`control.sock` and on configured listeners only, so a local user cannot read
30 days of the host's activity. `control.sock` is mode `0600` and additionally checks
`SO_PEERCRED` on every connection, rejecting any peer whose uid is not `0` with
`403 Forbidden` (`RequireRootPeer` in
`backend/monitoring/internal/httpapi/peer_gate.go`, over the
`backend/common/peercred` helper — the same uid gate the indexer applies on its
own socket). Commands are served only there; no configured listener ever mounts
`/api/v1/command`.

| Boundary | Owner |
|----------|-------|
| Collectors, delta trackers, live sampling | `linuxio-monitoring` |
| History persistence, aggregation, retention, maintenance | `linuxio-monitoring` SQLite store |
| Read-only metrics API and optional outside consumers | `linuxio-monitoring` listeners |
| Commands: config, status, SMART refresh, database checks | `linuxio-monitoring` control socket, root peers only |
| Complete wire contract shared by daemon and bridge: routes, socket constants, command and response envelopes, live, history and process types | `backend/monitoring/api` |
| Unix-socket clients and LinuxIO API adapters | `backend/bridge/handlers/monitoring` |
| Public generated bridge contract | `backend/bridge/apischema` |
| Hardware identity that needs a shutdown to change | `linuxio-bridge`, cached per session |
| Browser transport relay | `linuxio-webserver` |

Only `backend/monitoring/api` is importable outside the daemon; everything else
lives under `backend/monitoring/internal`: `app` (runtime, live and handoff),
`collector` (one package per collector plus the shared tick key, reseed policy
and single-flight helper), `live` (live payload assembly), `plugins` (plugin
catalogue, payload types, history parsing and averaging), `store` (SQLite
persistence only), `httpapi` (listeners and the root-peer gate), `config`
(YAML schema, `policy.go` defaults and limits, `env.go` environment knobs),
`daemon` and `cli` (command line), `deltatracker` and `units`.

## Installed files and units

| Path or unit | Purpose |
|--------------|---------|
| `/usr/libexec/linuxio/linuxio-monitoring` | Daemon binary, source `backend/monitoring/` |
| `/etc/linuxio/monitoring/config.yaml` | Strict YAML configuration, written by the daemon when absent |
| `/var/lib/linuxio/monitoring/metrics.db` | SQLite history store; `-wal` and `-shm` sidecars may appear beside it |
| `/run/linuxio/monitoring/api.sock` | Read-only metrics socket without history, mode `0666` |
| `/run/linuxio/monitoring/control.sock` | Metrics, history and commands, mode `0600`, root peer only |
| `linuxio-monitoring.service` | Long-running collector, store and API owner |

The unit is `Type=simple`, runs as `root` (SMART, the Docker socket, hwmon and
the GPU collectors need host devices), is `PartOf=` and `WantedBy=linuxio.target`,
and restarts `on-failure`. `ExecReload` sends `SIGHUP`. systemd owns
`RuntimeDirectory=linuxio/monitoring`, `StateDirectory=linuxio/monitoring` and
`ConfigurationDirectory=linuxio/monitoring`; `ProtectSystem=strict` leaves
`/var/lib/linuxio/monitoring` and `/etc/linuxio/monitoring` as the only writable
paths. The daemon is not socket-activated and has no idle exit: history has to
be sampled whether or not a browser is connected.

The data directory is fixed (`config.DataDir`, `/var/lib/linuxio/monitoring`).
There is no path setting and no command-line flag for it; the daemon creates
the directory at startup and fails fast if it cannot, and tests pass temporary
directories straight to the internal store.

## Command line

```text
linuxio-monitoring [--config PATH] [--verbose]
linuxio-monitoring --version
linuxio-monitoring --help
```

The first line is what the unit executes. There are no subcommands: status,
configuration, history and database maintenance are reached through LinuxIO,
which sends them as commands on the control socket. Logging goes to journald
through `backend/common/logging` under the syslog identifier
`linuxio-monitoring`.

## Sampling semantics

Two paths share one delta mechanism keyed by a `uint16` interval key.

**Collector tick.** Every `collector.interval` (default `1m`, at least `15s`)
the daemon collects the host plugins under one key, hands the finished sample
to live requests that were waiting during the run, writes the `1m` history row
of every plugin due on this tick, rolls up the coarser tiers whose bucket has
closed, and once an hour deletes what the tiers no longer keep (see
[History store](#history-store)). History values are averages over the
collector interval. The tick collects only what it stores: the process scan
runs only when `container_telemetry` history is due, and connections and IRQs
only when their own history is; a failed connection or IRQ read skips its own
row and not the tick. SMART refreshes on its own goroutine, because `smartctl`
can take 15 seconds per drive.

**Live request.** Every current endpoint, including `/api/v1/live`, keeps its
own sample key and its own delta baseline. Two rules apply to every live key:

1. **Reuse within 900 ms of a collection's start.** A request reuses its
   key's sample when that sample's collection began under 900 ms ago, and joins
   the collection when one is already in flight; otherwise it collects.
   `captured_at` (or `captured_at_ms`) reports the sample's own time, not the
   request time. One dashboard polling several cards therefore causes one
   collection per second, and several dashboards share it: a key never collects
   more than once per 900 ms. The window runs from the start of the collection
   and is shorter than the dashboard's one-second poll on purpose: counted from
   the finish with a full second, a request that returns one second later finds
   a sample one collection time (about 60 ms) short of a second old and is
   handed it again, so a one-second poll gets a new sample only every two
   seconds. A collection that took more than half the window is reused until it
   has rested as long as it took, so a host with slow sensors is not sampled
   back to back; there a one-second poll gets a new sample less often. See
   `ReuseWindow` in `backend/monitoring/internal/live`, `Fresh` in
   `backend/monitoring/internal/collector/flight.go` and their use in
   `backend/monitoring/internal/app/live.go`.
2. **A stale baseline reseeds from the collector.** When a live key's delta
   baseline is older than the last collector tick *and* that tick is itself at
   least one second old, that tick's counters replace the baseline before the
   delta is computed. A first request after an idle period averages over at most
   one collector interval instead of the whole idle time, and a key's first
   sample has a real window; the one-second condition (`collector.ReseedMinWindow` in
   `backend/monitoring/internal/collector/reseed.go`, shared by every manager)
   keeps a request that arrived just after a
   tick off a millisecond-wide window, which would read as zero CPU or as a rate
   amplified by 1000/δ. Each manager carries its own reseed helper:
   `reseedCPUFromCollector` for cpu, `network.Manager.reseedFromCollector` and
   `disk.Manager.reseedFromCollector` for network and disk, and
   `docker.Manager.ReseedFromCollector` for containers.

No live result is retained beyond the reuse window. A live request that
arrives while a collector pass is running waits on the app mutex; the
`/api/v1/{plugin}`, `/api/v1/all` and `/api/v1/system/summary` routes
additionally accept the finished collector sample through the handoff. A
payload that tick did not collect (processes, programs, container telemetry,
connections, IRQs) is not in the handed sample, and the request collects it
itself. The tick copies its container and filesystem records before it
releases the app mutex, as a live collection does, so a stored row never holds
a live request's one-second values.

Sleeping disks stay asleep: filesystem usage is served from the disk-usage cache
when `collector.disk_usage_cache` is non-zero, and SMART records are read on
their own refresh interval with `smartctl -n standby` (see SMART refresh below);
the one exception is a drive in standby that was never read, which is woken once
per daemon run so it appears. A tracked filesystem's `statfs` (the root and
`EXTRA_FILESYSTEMS`) runs on its own goroutine through the same
`disk.StatfsCalls` the live filesystem list uses: a collection waits a second
for the call it started and not at all for one still running, so a filesystem
that does not answer (a dead NFS server) keeps its last figures and their
`UsageAt`, logs one warning per stuck call, and does not hold the collection
lock. A call that returns after the wait is taken by a later collection while
it is at most an hour old (the live list's limit is 30 seconds), with the time
it returned as `UsageAt`, so a share that always takes a few seconds still
updates, every other collection. The root's disk is found from the mount table by device name or, for an
LVM or LUKS volume, by its device-mapper name; a filesystem label matches only
a name given in `FILESYSTEM` or an extra-filesystems folder, so another disk's
label cannot be taken for the root. Drive power readings have a 15-second cache. Each host collection also reads counters, CPU frequencies,
hwmon temperatures, GPU measurements and the adapter readings from `sensors -j`.
Missing sensor or SMART tools leave their optional sections empty.

The network collection reports the rates of every interface except `lo`,
`docker*`, `br-*`, `veth*`, `bond*` and `cali*` (or the ones `NICS` selects),
so the network page and the dashboard card have a history for a bridge, a
VLAN or a WireGuard interface. The stored total (`bandwidth`) adds only the
cards among them, the interfaces a device stands behind
(`/sys/class/net/<name>/device`: PCI, USB, virtio, platform): traffic through
a bridge, a VM's tap or a tunnel also crosses a card, and counting both
counted it twice (`isPhysicalNic` in `backend/monitoring/internal/collector/network`).
`NICS` in whitelist mode counts what it names, and a host with no card behind
any interface (LinuxIO in a container) counts every interface it reports.
Rows stored before this rule hold the old total until they age out.

The memory collection splits Docker and VM memory out of "used" by reading
cgroup v2 `memory.stat` files: every `system.slice/docker-*.scope` and
`docker/` for the two Docker cgroup drivers, and `machine.slice` for libvirt.
It counts only what a cgroup cannot give back without swapping (`anon`,
`shmem`, kernel stacks, page tables, per-CPU and unreclaimable slab), the same
pages "used" (total minus available) counts, so both figures are parts of it
and page cache stays out. Docker's own stats keep active page cache, so the
container list can show more than this split. A cgroup v1 host reads both as
zero. The ZFS ARC is still read from `/proc/spl/kstat/zfs/arcstats` and kept
out of "used". LinuxIO has no ZFS support in the UI, but the collector and the
ARC fields in the live and history payloads are kept on purpose: add new
memory fields beside them and do not remove them as unused code.

Live SMART records also include typed self-test history, NVMe health details
and power counters for the storage page. The history writer projects them
back to the original compact SMART fields before writing JSON to SQLite.

**SMART refresh.** Each refresh (every `collector.smart_refresh_interval`, or
`smart.refresh`) builds its list of drives from `smartctl --scan`,
`SMART_DEVICES` and the md arrays and eMMC devices in sysfs, reads each one and
replaces every record with what it read
(`backend/monitoring/internal/collector/smart`). A drive is read with
`-n standby`; smartctl's "Device is in STANDBY/SLEEP mode" message keeps the
drive's last record while the kernel's `diskseq` of that path is unchanged, so
a disk swapped in and asleep is not shown with the old disk's record, and a
drive in standby that was never read is woken once per daemon run so it
appears (a wake the daemon's stop cuts short is tried again). Exit bit 1 without that message is a drive that
could not be opened, which is a failure. A drive that fails or passes its
15-second limit is left out of the list until it answers again, and the refresh
goes on with the drives after it, md arrays and eMMC included; the storage page
then shows the drive without SMART data, not a warning, and the daemon logs a
warning when a drive starts failing or fails differently and an info line when
it is readable again. The status is smartctl's health result: `PASSED`,
`FAILED`, or `UNKNOWN` when smartctl reports none. A virtual disk (QEMU,
VirtualBox, Hyper-V and the like) is skipped with a debug line. The scan is cached for 30
minutes, a failure included, and a failed or empty scan keeps the previous
device list. A drive seen under two paths (one serial) is reported once, under
the first. A scan type of `scsi` or `ata` is detected by smartctl first and
`scsi` is then tried as `sat`; an NVMe controller path (`/dev/nvme0`) whose
health log cannot be read is tried again as its first namespace
(`/dev/nvme0n1`), which is queried first from then on. Temperatures and the
NVMe health counters are decoded into 64-bit types. A refresh that reads no
drive returns an error and writes no history row.

Disk I/O times (read, write, busy and weighted milliseconds) are differences of
`/proc/diskstats` counters the kernel prints as 32-bit numbers. A lower value
after one that fits in 32 bits is the counter wrapping, and the difference is
taken across the wrap (`ioTimeDelta` in
`backend/monitoring/internal/collector/disk/disk.go`); only a byte or operation
counter going down is a reset, which zeroes that sample.

A GPU tool that streams samples (`nvidia-smi -l`, `tegrastats`) is started
again whenever it exits: after 5 seconds, doubled for each run in a row that
gave no valid sample, up to 5 minutes; it never stops for good
(`gpuCollector.start` in `backend/monitoring/internal/collector/gpu/manager.go`).
A line the parser rejects is skipped, and the first one of a run is logged as a
warning. Between two samples a GPU reports its last average; a GPU without
engine readings whose temperature and memory read zero is taken as suspended
and reports zeros, while an Intel GPU, which reports engines but no temperature
or memory, keeps its last average.

Docker and Podman reads use the repository's Moby client for API negotiation,
container listing, one-shot stats, version, host info and health inspection.
The daemon retains its per-key CPU and network baselines, memory-cache
subtraction, exclusion patterns and request deadlines. Docker 25 and newer
and Podman use at most five concurrent stats requests; older Docker versions
retain the batch behavior required by their one-shot stats endpoint. The
stats pass samples the listed containers that are running or paused and skips
the rest with a debug line: the list also returns a container Docker is
restarting, which has no process between two runs, and asking for its stats
logged an error on every cycle of a restart loop. One pass, the list included,
has a deadline of five seconds, or twice `DOCKER_TIMEOUT` when that is longer,
so a daemon that stops answering holds the collection lock that long and no
longer. A container whose stats read fails is asked again, up to five tries in
all within that deadline; one that has not answered is left out of the sample
with one warning naming it, rather than drawn as zeros (a container that stops
between the list and its stats read answers with empty memory figures). A
container's CPU and network baselines are dropped once in its first minute,
on the first pass that sees "Up ... seconds", so a restart under the same ID
starts from the new run's counters and the later passes of that minute have a
figure; a second restart inside that minute sends the counters backwards,
which reads as zero for one pass. With `DOCKER_HOST` unset the daemon looks
for `/var/run/docker.sock`, then root's Podman service at
`/run/podman/podman.sock`, then `/run/user/0/podman/podman.sock`. Podman is
monitored only: the Containers pages, Compose and image updates speak to
Docker.

## History store

`metrics.db` holds one table per history-capable plugin, with the primary key
`(resolution, captured_at)` and no other index: reads, rollups and retention
all name a resolution and a range of times. The history is stored in four
tiers (`backend/monitoring/internal/store/store.go`):

| Resolution | Kept for | Rows per table |
|------------|----------|----------------|
| `1m` | 1 day | 1,440 at the default interval |
| `10m` | 12 hours | 72 |
| `20m` | 1 day | 72 |
| `60m` | `history.retention` (default 30 days) | 720 |

The collector writes the `1m` rows. When a tick crosses a bucket boundary of a
coarser tier, the tier's row for each closed bucket is written from the rows
the tier below holds for that bucket, averaged as `plugins.Average` does (each
row decoded once into its record; a plugin without an average, such as
`smart`, keeps the newest row). A rollup row's time is the time of the newest
row it averages. A bucket is rolled up once: the next one starts after the
newest row its tier holds, so a restart neither repeats a bucket nor skips one
whose rows are still kept. A read returns the stored rows of the tier it asks
for, newest `limit` first and then in time order; nothing is averaged at read
time, so a coarse tier's newest row can be up to one step old. The UI's ranges
read `1m` for an hour, 12 hours and a day, and `60m` for 7 and 30 days. The
container history reads the `10m` and `20m` tiers for 12 hours and a day
(`rollupResolution` in `frontend/src/components/charts/historyRanges.ts`): a
day of minute rows of some 60 containers would pass the 16 MiB frame a bridge
response travels in. A request asks for at most 6,000 rows
(`config.MaxHistoryLimit`, and the bridge's `maxHistoryLimit`), which fits a
day of minute rows at the shortest collector interval (15 s: 5,760). A day of
`1m` rows is about 11 MB over all tables at today's row sizes (estimated, not
measured).

`history.retention` is the hourly tier's length and bounds every tier: a tier
keeps the shorter of its own length and the retention. The delete runs once an
hour on the collector goroutine, so the `1m` tier holds up to 25 hours of
rows, and `db.maintain` runs it at once.

Containers in a rollup follow Beszel: a container is keyed by name and keeps
the newest record's ID, so one recreated by an image update stays one series,
and its CPU, memory and network are divided by every row in the bucket, so one
minute at 100% CPU in a ten-minute bucket reads 10%, the share of the bucket it
used. The `container_telemetry` rollup keys by name too and divides its CPU and
disk rates the same way, since the container history joins its disk rates to
the containers rollup; its process count and GPU readings average over the
rows that carry them. Rows rolled up before this rule keep the old one (by ID,
over the samples a container appeared in) until they age out.

The store's layout has a version (`storeSchemaVersion`, now 8). A database of
any other version, older or newer, is deleted with its sidecars at start and
created again empty, so an upgrade that changes the layout starts the charts
empty, and a downgrade does not leave the daemon restarting for good.

## HTTP API

This is an in-repository service contract, not a network or third-party API.
Every route below is served on both fixed sockets, except the history route,
which is mounted on `control.sock` and on configured listeners only, and
`/api/v1/command`, which is served only on `control.sock`. Responses are JSON; errors use an HTTP status and a
JSON error body.

| Method and path | Purpose |
|-----------------|---------|
| `GET /healthz` | Collector liveness: `healthy`, `last_updated`, `age_seconds` |
| `GET /api/v1/meta` | Version, data directory, database path and size (`db_size_bytes`), live listeners, intervals, retention |
| `GET /api/v1/plugins` | Plugins mounted on this listener and their routes |
| `GET /api/v1/all` | Current sample for every mounted plugin except `processes` and `programs`, which have their own routes, and `container_telemetry`, which needs a process scan and is included only with `?include=container_telemetry` |
| `GET /api/v1/{plugin}` | Current sample for one plugin |
| `GET /api/v1/{plugin}/history` | Stored history for one history-enabled plugin; not on `api.sock` |
| `POST /api/v1/{plugin}/refresh` | Forces a refresh; only `smart` implements it, and only the control socket mounts it |
| `GET /api/v1/system/summary` | Cross-plugin summary; mounted only on unrestricted listeners |
| `GET /api/v1/live` | The LinuxIO-shaped live payload, see below |
| `POST /api/v1/command` | One command; `control.sock` only |

Plugin names are `cpu`, `mem`, `swap`, `load`, `diskio`, `fs`, `network`, `gpu`,
`sensors`, `containers`, `container_telemetry`, `processes`, `programs`,
`connections`, `irq` and `smart`. `processes` and `programs` are live-only and
never persisted.

The `cpu` history row holds the total percent, the maximum percent, the
breakdown, the per-core percent and the per-core `cpu_frequencies_mhz`, each
averaged over the collector tick; the bridge surfaces the frequencies as
`frequencies_mhz` on `monitoring.get_cpu_history`.

The `mem` history row adds `memory_docker_gb` and `memory_vm_gb`, the Docker
and VM parts of `memory_used_gb`; `monitoring.get_memory_history` surfaces
them as `docker_used_gb` and `vm_used_gb`, and the hardware page stacks System
(the rest of used), Docker, VMs and, when non-zero, the ZFS ARC.

The `sensors` history row keeps the hwmon `temperatures` and adds `readings`:
every numeric input reading from `sensors -j` (temperatures, fans, voltages,
power, current), keyed by adapter and then by the label the live `sensors`
section shows. Limits, alarms and flags stay live-only. Live requests read
sensors on demand, so only a collector tick on which `sensors` history is due
runs `sensors -j` for this row. `monitoring.get_sensor_history` returns the
readings per point.

`/healthz` answers from memory: it compares the last collector tick against the
configured interval and returns `503 Service Unavailable` only when no tick has
happened yet or the last one is older than twice the interval. A freshly started
daemon is healthy immediately, because `runtime.go` runs one `collectAndPersist`
before it opens the sockets. There is no health file.

### `GET /api/v1/live`

`api.Live` (`backend/monitoring/api/live.go`) is a byte-precise, LinuxIO-shaped
payload built for the bridge, which reads it over `api.sock`
(`monitoring.FetchLive`). The unprivileged `monitoring.get_live` bridge route
embeds this contract in the generated frontend API. Dashboard, hardware, network
and storage views select measurements from this shared query:

| Field | Content |
|-------|---------|
| `captured_at_ms` | Sample time in Unix milliseconds |
| `uptime_seconds` | Host uptime |
| `cpu` | Total percent, per-core percent, breakdown (user, system, iowait, steal, idle), three load averages, per-core MHz and CPU temperatures: `package` and `core<N>`, or on a host with several CPU chips `package<i>` and `package<i>core<N>` per chip, in the order `sensors -j` lists them |
| `memory` | Total, used, available, free, cached, buffers, shared, swap total, swap free, ZFS ARC, and the Docker-used and VM-used parts of used |
| `disks` | Per physical block device: read and write bytes per second and operations per second |
| `disk_io` | Host totals across those devices |
| `interfaces` | Per interface: rx and tx bytes per second, byte and packet totals, errors and drops |
| `filesystems` | Per mount: device, mountpoint, filesystem type, byte usage, inode usage and read-only state. Pseudo filesystems (`proc`, `sysfs`, `tmpfs`, `overlay`, `binfmt_misc`, `efivarfs`, `hugetlbfs`, `mqueue`, `rpc_pipefs` and the like) are left out. Each mount's `statfs` runs on its own goroutine, at most one per mount: a collection waits a second for the call it started and not at all for one still running, so a mount that does not answer (a dead NFS server) is missing from the list until its call returns, and costs nothing else |
| `sensors` | Adapter groups and their labeled numeric or boolean readings |
| `gpus` | GPU measurements keyed by PCI address, or collector ID when no address is available. `nvidia-smi` is asked for the driver version, memory size, power limit and top clock once every 10 minutes, and for the changing figures on every collection |
| `smart` | Typed SMART records keyed by device name, with cached drive power readings |
| `containers` | Own `captured_at_ms` and items with id, name, CPU percent (Docker's multi-core convention), memory bytes, rx and tx bytes per second, and block read and write bytes per second when container telemetry is fresh: refreshed by the ticks that write `container_telemetry` history and fresh for three of its intervals, so with that history off the items carry no block I/O |

The bridge caches successful CPU, GPU and motherboard identity reads for its
process lifetime. Hosts without DMI expose empty motherboard fields so widgets
can use system identity fallbacks or show unavailable values. Other DMI read
errors remain retryable. Frontend identity queries use a day-long stale time
without periodic refetching. GPU views join live measurements to that identity by PCI
address. Drive inventory and network configuration remain managed bridge data.

### Processes and programs

The unprivileged `monitoring.get_processes` and `monitoring.get_programs` bridge
routes read `/api/v1/processes` and `/api/v1/programs` through `api.sock`.
Processes include command arguments, user, status, threads, CPU and memory
usage, disk I/O rates and container attribution. The response also includes
the daemon's process counts. Programs group CPU and memory usage by name and
include their process count and PIDs.

A scan reads each process's `stat`, `status`, `cmdline` and `io` once
(`backend/monitoring/internal/collector/process/procfs.go`), and its `cgroup`
for attribution, and looks a user name up once per uid; a uid with no name
shows as its number. CPU percent is user plus system time over the time since
the previous scan, or since the process started on its first; nice is the
value in `stat`; the memory figures are `status`'s `Vm*` lines. A process
counts for a container only when its cgroup is the one a runtime running as
root makes for it, named by the full container ID where only root can create
it: `/system.slice/docker-<id>.scope` or `/docker/<id>` for Docker's two
cgroup drivers, `/machine.slice/libpod-<id>.scope` or
`/libpod_parent/libpod-<id>` for Podman's, so a cgroup a local user makes
under their own slice and names after a container is never counted.

The Services section shows them in two read-only tabs that poll every two
seconds while open. `/services/programs` holds the virtualized program table,
with sorting and a `?filter=` text filter. `/services/htop` adds an htop-style
header from `monitoring.get_live` (one meter per core, memory split into used,
buffers and cache, swap, tasks, load average and uptime) above the process
list ordered by CPU. Process actions are not available on either tab.

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
| `filesystems` | `fs` |
| `sensors` | `sensors` |
| `gpus` | `gpu` |
| `smart` | `smart` |
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

The bridge exposes `smart.refresh`, `db.check` and `db.maintain` as
`monitoring.refresh_smart`, `monitoring.check_database` and
`monitoring.maintain_database`. These routes require a privileged session and
send commands through `control.sock`.

`config.set` reports `restart_required: true` whenever the request carries a
`listeners` key, identical or not, because listener changes only take effect on
a restart. `config.reload` reports it when the file's listeners differ from the
running ones.

## Configuration

`/etc/linuxio/monitoring/config.yaml` is strict YAML parsed with the
repository's `goccy/go-yaml` (`backend/monitoring/internal/config/config.go`).
Unknown keys, multiple documents and invalid values are rejected, so a release
that removes a key keeps accepting it; an absent file means defaults, which the
daemon then writes. The defaults live in
`backend/monitoring/internal/config/policy.go` and are:

```yaml
version: 1
collector:
  interval: 1m0s            # history sampling cadence
  smart_refresh_interval: 1h0m0s
  disk_usage_cache: 0s      # 0 re-reads usage on every collection; set e.g. 15m to keep sleeping disks asleep
history:
  retention: 720h0m0s       # how long hourly history is kept; see History store
  plugins: [cpu, mem, swap, diskio, network, sensors, containers, container_telemetry]
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
| `collector.interval` | Live, on `config.set` and `SIGHUP`; at least `15s` |
| `collector.smart_refresh_interval` | Live |
| `collector.disk_usage_cache` | Live |
| `history.retention` | Live |
| `history.plugins` | Live; an empty list records nothing |
| `history.intervals` | Live; a plugin's row is written on every Nth collector tick |
| `listeners` | Needs a service restart |

`version` must be `1`. `collector.interval` must be at least `15s`
(`MinCollectorInterval`: every tick writes a history row per plugin), and
`collector.smart_refresh_interval` and `history.retention` must be greater than
zero; `disk_usage_cache` may be `0s`, and no duration may be negative. A file
that fails these checks stops the daemon at start, and a reload leaves the
running configuration alone, so a file saved with an interval below `15s` by
an earlier release has to be corrected before the daemon starts again. Each
`history.intervals` entry names a history-capable plugin other than `smart`
(whose history follows `collector.smart_refresh_interval`) and must be a whole
multiple of `collector.interval`; the stored value is still the average over one
collector tick, sampled every Nth tick. Listener
names and addresses must be unique, `api` and `control` are reserved names, the
two fixed socket paths are reserved addresses, and every named plugin must
exist. An address may be
`host:port`, a bare port (which binds `127.0.0.1`), `unix:/path`, or an absolute
path; a configured unix socket is mode `0660`.
The settings UI edits a TCP listener as a port only and writes `:port`, which
binds every interface; a host-bound or unix address set in the file is shown
read-only there.

Configured listeners serve the read-only metrics API, history included, and are
**unauthenticated**: anything that can reach the address can read what the
listener exposes, and so can any local user through the loopback. A listener
that names no plugins serves all of them, the stored history and every
process's command line included; the plugin allowlist is how a listener serves
less. They never serve commands. The settings page says so beside the port
field. `SIGHUP`
(`systemctl reload linuxio-monitoring`) re-reads the file and applies the live
fields; a bad file leaves the running configuration untouched and logs the
error.

The fork's remaining knobs stay environment-only and are not exposed in the UI,
because they are host facts rather than timings. The complete list is the
constants in `backend/monitoring/internal/config/env.go`; nothing else in the
daemon reads the environment (there are no `DATA_DIR` or `HISTORY` variables): `NICS` (interface filter),
`SENSORS`, `SYS_SENSORS`, `PRIMARY_SENSOR` and `SENSORS_TIMEOUT`,
`GPU_COLLECTOR`, `SKIP_GPU` and `INTEL_GPU_DEVICE`, `SMART_DEVICES`,
`SMART_DEVICES_SEPARATOR` and `EXCLUDE_SMART`, `EXTRA_FILESYSTEMS` and
`FILESYSTEM`, `MEM_CALC`, `DOCKER_HOST`, `DOCKER_TIMEOUT` and
`EXCLUDE_CONTAINERS`, and `HTTP_LOG` for per-request logging at debug level. Set them with a
systemd drop-in on `linuxio-monitoring.service`. An invalid `DOCKER_HOST` or
`DOCKER_TIMEOUT` disables Docker monitoring with a logged error instead of
stopping the daemon, and `https://` Docker hosts are refused because the
client speaks plain HTTP. `allow_remote_commands` does not exist.

## Capability and failure behaviour

The `monitoring` capability ships with LinuxIO and is not installable from
Capability Manager. Detection is a `GET /healthz` on `api.sock`
(`checkMonitoringAvailability` in
`backend/bridge/handlers/capabilities/probes.go`), so the capability reports
whether the daemon is running and has produced a sample, nothing about
packages.

`lm_sensors` and `smartmontools` remain ordinary bridge-detected, installable
capabilities, and the frontend keeps gating the sensor and SMART views on them.
The daemon reads CPU temperatures through hwmon, adapter readings through
`sensors -j`, and SMART records through `smartctl`. Missing helpers leave the
corresponding optional readings empty.

A daemon that is down does not break unrelated features: the bridge's public
live route returns zero measurements with an unset capture time, process and
program routes return empty lists, and Docker container metrics report
`unavailable`. Privileged history, status and configuration routes surface the
error to the UI. See
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
drop-ins. Either change reaches the daemons live (SIGHUP reload, no restart); bridges of
already-open sessions switch at the next login. It persists across restarts.
The same switch is in Settings.

If the database has to be recovered manually, stop the service, move
`/var/lib/linuxio/monitoring/metrics.db*` aside, and start it again — the daemon
creates an empty store and begins recording from the next tick. Move rather than
delete, so the old history stays available for inspection. The daemon itself
moves a database SQLite reports as corrupt aside at start, as
`metrics.db.corrupt-<time>`, and keeps only the newest copy it named that way;
a copy given another name is left alone.

## Known limits

- The NVIDIA collector reads a GPU's bus id through `nvmlDeviceGetPciInfo_v3`,
  or the unversioned `nvmlDeviceGetPciInfo` of a library that does not export
  it (`backend/monitoring/internal/collector/gpu/nvml.go`): not checked on a
  host with an NVIDIA GPU.
- A drive that hangs smartctl inside the kernel holds the SMART refresh past
  the 15-second limit until the kernel gives up on the command: the kill takes
  effect only then, and the drives after it wait.
- An NVMe drive whose firmware reports a health counter past 64 bits has no
  health log in its record; the storage page lists its attributes instead,
  where that counter reads `invalid (out of range)`.
- Whether `smartctl --scan` and `smartctl -a` can name different types for one
  disk was not checked across smartctl versions; the collector follows Beszel's
  handling.
- Drives behind one controller path (`/dev/bus/0` with `megaraid,0`,
  `megaraid,1`) share one key in the live payload, so only one shows; the
  storage page joins SMART to drives by block device name, which such drives
  do not have.
- A container whose runtime does not run as root, or that was given another
  `--cgroup-parent`, has no processes attributed to it and so no container
  telemetry.
- Sensors with the same name are told apart by their position in the list,
  which changes when another sensor appears.
- A GPU whose collector has stopped keeps reporting its last average, live and
  in history, until the collector runs again (after up to five minutes when it
  keeps failing).
- A configured unix-socket listener is mode `0660` and owned by root, with no
  group setting, and the unit's sandbox leaves almost nowhere writable to put
  one.
- The daemon binds its two sockets itself after the first collection, so they
  do not exist until then; the bridge retries until they do.

## See Also

- [Process & Systemd Architecture](process-systemd-architecture.md) — where this
  unit sits among the LinuxIO processes and privilege boundaries.
- [Capabilities](capabilities.md) — detection, gating and the install flow.
- [Filesystem Indexer](indexer.md) — the other first-party daemon, same socket
  and privilege pattern.
