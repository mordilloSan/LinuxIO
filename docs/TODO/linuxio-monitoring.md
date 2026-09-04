# linuxio-monitoring: import go-monitoring as a first-party service

> **Status: Plan 1 implemented; Plans 2 to 4 pending.** `linuxio-monitoring` is
> now a first-party binary shipped with LinuxIO: the daemon, its two fixed
> sockets, the strict YAML config, the `/api/v1/live` payload's first sections
> and the repointed bridge routes are in place. The remaining plans move the
> live gauges, the moved-in bridge collectors (filesystems, sensors, GPU,
> SMART), the process view and the moby Docker client. The external APT
> capability install path is gone.

Design settled on 2026-09-03. go-monitoring's repository is archived after the
import; LinuxIO is the only home of the code from then on. There are no other
users or installations to migrate.

## Goal

One binary is responsible for both live sampling and long-term storage of host
metrics. The bridge stops reading anything that changes over time and becomes a
thin client of the daemon. Hardware identity that cannot change without a
shutdown stays on the bridge and is read once per login session.

## Ownership model

| Concern | Owner |
|---|---|
| Sampling CPU, memory, load, disk I/O, network, filesystem usage, sensors, GPU, SMART, containers | `linuxio-monitoring` |
| History persistence, retention, aggregation | `linuxio-monitoring` SQLite store |
| Read-only metrics API for LinuxIO and optional outside consumers | `linuxio-monitoring` listeners |
| Commands: config, status, SMART refresh, database maintenance | `linuxio-monitoring` control socket, root callers only |
| Process and program listing | `linuxio-monitoring` live process manager, read through the bridge |
| Hardware identity: CPU model, memory modules, motherboard, BIOS, PCI, GPU identity, host info | `linuxio-bridge`, once per session |
| Managed objects: containers, mounts, LVM, services, users, shares, network configuration, updates, health alerts | `linuxio-bridge`, as today |
| Live and history wire types shared by daemon and bridge | `backend/monitoring/api` |
| Public generated bridge contract | `backend/bridge/apischema` |
| Browser transport relay | `linuxio-webserver`, unchanged |

## Boundary rule

- **Sampled measurements of the machine** go through monitoring. A value that
  the kernel, a device or a workload changes on its own is a measurement.
- **Hardware identity** that needs a shutdown to change stays on the bridge. The
  bridge caches it for the process lifetime, and the frontend requests it once
  with a day-long stale time and no refetch interval.
- **Managed objects** the bridge itself mutates stay on the bridge and refresh
  through the existing operation invalidations and polling. They are state, not
  measurements.

## Process model

| Item | Value |
|---|---|
| Binary | `/usr/local/bin/linuxio-monitoring`, source `backend/monitoring/` |
| Unit | `linuxio-monitoring.service`: root, `PartOf=` and `WantedBy=linuxio.target`, `Restart=on-failure`, `ExecReload` sends SIGHUP. Long-running, not socket-activated, no idle exit. Hardening copied from the go-monitoring unit with `RuntimeDirectory`, `StateDirectory` and `ConfigurationDirectory` set to `linuxio/monitoring`. |
| Read socket | `/run/linuxio/monitoring/api.sock`, mode 0666, serves the metrics API. Any local process may connect, matching today's loopback TCP default and the visibility of `/proc`. |
| Control socket | `/run/linuxio/monitoring/control.sock`, mode 0600, serves commands and metrics. A connection middleware reads `SO_PEERCRED` and rejects any peer that is not uid 0, reusing the indexer's helper. |
| Optional listeners | Configured `listeners`, default empty. TCP or unix, read-only metrics API only, each with an optional plugin allowlist. Unix sockets keep mode 0660. Unauthenticated, as before. |
| Database | `/var/lib/linuxio/monitoring/metrics.db` |
| Config | `/etc/linuxio/monitoring/config.yaml`, strict YAML, version 1, created by the daemon when absent |
| CLI | `run [--config PATH] [--verbose]`, `--version`, `--help`. No other commands. Status, configuration, database maintenance and SMART refresh are reached through bridge routes. |
| Logging | `backend/common/logging` journald handler, identifier `linuxio-monitoring` |
| Profiling | `backend/common/debugserver`, debug builds only |

## Sampling semantics

Two paths share one delta mechanism keyed by a `uint16` interval key, as
go-monitoring does today.

**Collector tick**, every `collector_interval` (default 1 minute). Gathers all
plugins under key `60000`, hands the finished sample to live requests that were
waiting during the run, writes the history-enabled plugins to SQLite, refreshes
SMART when due, runs retention and maintenance. History values are averages over
the collector interval. Unchanged.

**Live request** on any current endpoint, including the new `/api/v1/live`.
Each endpoint keeps its own key and its own delta baseline. Two changes apply to
every live key:

1. **Reuse within one second.** A request reuses a live sample for its key that
   is under one second old or still being collected. Otherwise it collects. This
   extends the existing collector handoff from "during the tick" to "within the
   last second". One dashboard polling several cards causes one collection per
   second; several dashboards share it. `captured_at` reports the sample time.
2. **Stale baseline reseeds from the collector.** When a live key's baseline is
   older than the last collector tick and that tick is itself at least one
   second old, the baseline is replaced by that tick's counters before the delta
   is computed. A first request after an idle period averages over at most one
   collector interval instead of the whole idle time, and a key's first network
   sample has a real window; the one-second condition keeps a request that
   arrived just after a tick off a millisecond-wide window, which would read as
   zero CPU or as a rate amplified by 1000/δ. Applies in the cpu, network, disk
   and docker managers.

Nothing else changes: the whole system sampler still runs per collection under
the app mutex, containers are queried only when requested with the existing
Docker timeout, processes and programs use the live process manager, and no
result is retained beyond the one-second reuse.

Filesystem usage is served from the daemon's disk-usage cache so sleeping disks
stay asleep. SMART data is served from the SMART cache on its own refresh
interval. Drive power state is read at most every 15 seconds. Only counters and
hwmon or `sensors -j` readings are sampled per collection.

## HTTP API

The go-monitoring REST API is kept as is: `/healthz`, `/api/v1/meta`,
`/api/v1/plugins`, `/api/v1/all`, `/api/v1/{plugin}`,
`/api/v1/{plugin}/history`, `/api/v1/{plugin}/refresh`,
`/api/v1/system/summary`, and `/api/v1/command` on `commands` listeners. Removed:
`/api/v1/benchmark` and the `debug` API kind.

New: `GET /api/v1/live`, mounted on every metrics listener. Returns
`api.Live`, a LinuxIO-shaped, byte-precise payload:

| Section | Content |
|---|---|
| `captured_at_ms` | sample time |
| `uptime_seconds` | host uptime |
| `cpu` | total percent, per-core percent, breakdown (user, system, iowait, steal, idle), load average, per-core current frequency in MHz, CPU temperatures keyed `package` and `coreN` |
| `memory` | total, used, available, free, cached, buffers, shared, swap total, swap free, ZFS ARC, Docker used, all bytes |
| `disks` | per physical block device: read and write bytes per second, read and write operations per second; plus host totals. Uses the bridge's current physical-disk filter, moved into the daemon. |
| `interfaces` | per interface: rx and tx bytes per second, rx and tx byte totals |
| `filesystems` | per mount: device, mountpoint, fstype, total, used, free, used percent, inode counts, read-only. Same shape as today's `FilesystemInfo`. |
| `sensors` | groups by adapter with readings, same shape as today's `SensorGroup`. Produced by the bridge's `sensors -j` parser moved into the daemon's sensors plugin. |
| `gpus` | keyed by PCI address: utilization, temperature, memory used and free, power draw and limit, current, requested, actual and boost frequencies, fan percent and RPM, RC6 residency, power state, runtime status, connected displays. Produced by the bridge's sysfs and DRM readers moved into the daemon, merged with the existing NVML, AMD, Intel and nvtop collectors where those report a PCI bus id. A vendor-collector entry with no PCI address is listed under its collector id and is not joined to a static GPU. |
| `smart` | keyed by device name: the daemon's typed SMART record and the drive power state |
| `containers` | own `captured_at_ms` and items with id, name, CPU percent, memory bytes, rx and tx bytes per second, block read and write bytes per second when telemetry is available |

`api.Live` lives in `backend/monitoring/api` and is embedded into
`backend/bridge/apischema` the way `indexerapi.IndexerConfig` is today, so the
generated frontend types follow it.

### Plugin allowlist on configured listeners

A configured listener may name the plugins it exposes. On such a listener only
the allowed plugin routes are mounted, `/api/v1/all` and `/api/v1/live` return
only the allowed plugins and sections, `/api/v1/plugins` lists only those, and
`/api/v1/system/summary` is mounted only when the list is unrestricted. Live
sections map to plugins as follows: `cpu` to cpu, `memory` to mem and swap,
`disks` to diskio, `interfaces` to network, `filesystems` to fs, `sensors` to
sensors, `gpus` to gpu, `smart` to smart, `containers` to containers and
container_telemetry; `uptime_seconds` is always present. The two fixed unix
sockets are unrestricted.

## Config

Strict YAML in the indexer's style, parsed with the repo's `goccy/go-yaml`,
unknown keys rejected. go-monitoring's validation logic is kept; its JSON loader
and `Duration` type are replaced by the YAML loader.

```yaml
version: 1
collector:
  interval: 1m              # history sampling cadence
  smart_refresh_interval: 1h
  disk_usage_cache: 0s      # 0 re-reads usage on every collection; set e.g. 15m to keep sleeping disks asleep
history:
  retention: 720h
  plugins: [cpu, mem, swap, diskio, network, containers, container_telemetry]
  intervals: {}             # per plugin, e.g. containers: 5m; a whole multiple of collector.interval
listeners: []               # empty means no TCP; the unix sockets are always on
# listeners:
#   - name: homepage
#     address: 0.0.0.0:45876
#     plugins: [cpu, mem, network]   # omitted means all metrics plugins
```

- `collector.interval`, `collector.smart_refresh_interval`,
  `collector.disk_usage_cache`, `history.retention`, `history.plugins` and
  `history.intervals` live-apply on `config.set` and on SIGHUP. `listeners` changes require a
  restart, as today. `disk_usage_cache` replaces the `DISK_USAGE_CACHE`
  environment variable.
- `allow_remote_commands` is removed: commands are served only on the control
  socket, which sits behind LinuxIO login and the bridge's privileged-route
  check. The two fixed sockets are not configurable and do not appear in
  `listeners`.
- The remaining go-monitoring environment knobs stay environment-only and are
  documented: NIC filters, sensor filters and timeout, GPU collector choice and
  skip, SMART device lists, extra filesystems, memory formula, Intel GPU device.
  They are not timings and the UI does not expose them.
- The settings section edits every file field: intervals, disk usage cache,
  retention, history plugins with a per-plugin interval, and listeners with
  address and plugin selection.

## Bridge changes

### Monitoring handler package

- Two fixed clients: `api.sock` for `monitoring.get_live` and capability
  detection, `control.sock` for every privileged route. The current lookup of
  the metrics listener through the status response is deleted.
- New route `monitoring.get_live`, unprivileged, `RetrySafe`, returning
  `apischema.MonitoringLive`. When the daemon is unreachable it returns the
  payload with zero values and an unset `captured_at_ms`; it does not error.
- New routes `monitoring.get_processes` and `monitoring.get_programs`,
  unprivileged, `RetrySafe`, calling `/api/v1/processes` and
  `/api/v1/programs` on `api.sock`. Process items carry PID, name, command
  line, user, status, threads, CPU percent, memory RSS and percent, disk read
  and write bytes per second, and container id and name; the processes
  response also carries the daemon's process counts. Programs carry name,
  count, CPU percent, memory percent, RSS bytes and PIDs. Both return empty
  lists when the daemon is unreachable.
- Container metrics for `docker.list_containers` are taken from the live
  `containers` section instead of two HTTP fetches. Item validation stays.
- History, config, status and restart routes move to `control.sock`. Restart
  targets `linuxio-monitoring.service`. `MonitoringConfig` and its patch lose
  `allow_remote_commands`. Status lists the two fixed sockets and configured
  listeners.

### Routes that move, shrink or go

| Route | Change |
|---|---|
| `system.get_cpu_info` | Static only: vendor, model name, family, model, base MHz, core count. Cached for the bridge lifetime. Usage, load, frequencies and temperatures come from `monitoring.get_live`. |
| `system.get_memory_info` | Deleted. Totals and usage are in the live `memory` section. |
| `system.get_disk_throughput` | Deleted. Live `disks`. |
| `system.get_uptime` | Deleted. Live `uptime_seconds`. |
| `system.get_fs_info` | Deleted. Live `filesystems`. Operation invalidations that name it invalidate `monitoring.get_live` instead. |
| `system.get_sensor_info` | Deleted. Live `sensors`. |
| `system.get_gpu_info` | Static only: address, ids, vendor, model, driver, driver version, DRM card, VRAM total, link maxima. Cached for the bridge lifetime. Live fields come from the live `gpus` section joined on address. |
| `system.get_motherboard_info` | Temperatures removed. Baseboard and BIOS stay, cached. |
| `network.get_interface_stats` | Deleted. Rates come from live `interfaces`; addresses, MAC and link speed already come from `network.get_network_info`. |
| `storage.get_drive_info` | SMART and power fields removed. Inventory, model, serial, size, type stay, since USB drives hotplug. Self-test actions stay bridge mutations. |
| `system.get_processes` | Deleted. Replaced by `monitoring.get_processes` and `monitoring.get_programs`, see the process view below. |

### Static route caching

Static routes reuse the pattern of `hw_cache.go`: a successful load is kept for
the process lifetime, failures retry on the next call. The frontend requests
them with `staleTime: CACHE_TTL_MS.ONE_DAY` and no `refetchInterval`. The
motherboard card's slow refetch and the hardware page's GPU refetch go.

### Capability

- `monitoring` detection calls `/healthz` on `api.sock`.
- The install spec, `OptionalComponentMonitoring`, the installer download code
  and its test are deleted. The frontend capability entry becomes built-in:
  not installable, dependency `linuxio-monitoring`, updated texts.
- `lm_sensors` and `smartmontools` stay bridge-detected capabilities. When the
  `sensors` or `smartctl` binaries are missing the daemon leaves the `sensors`
  and `smart` sections empty; the frontend keeps gating on the capabilities.

### CLI

`linuxio logs monitoring` filters on the new unit and syslog identifier.
`linuxio version` lists the new binary. `linuxio restart --full` already covers
it through the target. The man page follows.

## Frontend changes

- Regenerate the client after the schema changes.
- Dashboard cards read `monitoring.get_live` with `select` and poll at
  `DASHBOARD_REFETCH_FAST_MS`. Processor combines the static CPU route with the
  live cpu section. Memory, disk, network and filesystem cards read live only.
  System overview reads uptime from live. The memory refetch constant goes.
- Hardware page: sensors and GPU live fields come from live; identity from the
  static routes.
- Storage disk overview reads SMART and power state from the live `smart`
  section and adapts its field access to the typed record.
- Live-series backfill and history cards are unchanged.
- Settings section: remote-commands toggle removed; fields for collector
  interval, SMART refresh interval, disk usage cache, retention and history
  plugins; the listener editor holds name, address and a plugin selection, with
  a note that listeners are unauthenticated; status shows the fixed sockets.
- Query-ownership tests and card query-ownership tests are updated for the new
  routes. Comments naming go-monitoring or 15-second samples are corrected.

## Process view

- New authenticated route `/processes` with a sidebar entry.
- A virtualized table over `monitoring.get_processes`, polled every 2 seconds,
  with a processes/programs toggle backed by `monitoring.get_programs`, column
  sorting and a text filter on name, command line and user. Columns: PID, name,
  user, CPU percent, memory RSS, disk read and write per second, threads,
  container. The header shows the daemon's process counts.
- Read-only. No kill or signal action.
- The daemon's live process manager samples per request; the one-second reuse
  applies to its key like any other live key.

## Import: what is copied, kept and dropped

**Copied** into `backend/monitoring/` with import paths rewritten: `app`,
`store`, `config`, `api/http`, `api/model`, `domain/*` except `systemd`,
`integration/docker`, `deltatracker`, `utils`, `defaults`, and their tests.

**Kept as is:** collectors, store and history aggregation, config validation
and SIGHUP reload, command API, HTTP server and listener code, collector
handoff extended as described above. The docker integration is kept for the
import and replaced in the final phase, see below.

**Dropped:** the `cmd` package (menu, status, db and config commands), its
logging and journald handler, the health file package (`/healthz` reports the
last collector tick age from memory), benchmark and pprof routes, `leakcheck`,
the systemd plugin and its domain package, its version package, the JSON config
loader, `packaging/` and `scripts/`, `allow_remote_commands`, and the implicit
loopback-TCP default listener.

**Final phase, Docker client:** once everything above is green, the raw Docker
Engine client and its DTOs in `integration/docker` are replaced by the
`github.com/moby/moby/client` the bridge already uses. Container listing,
one-shot stats, version and info calls move to the typed client; the delta
trackers, Podman detection, the concurrency limit and the exclusion patterns
stay. Parity is checked against the existing fixture JSON under
`integration/docker/testdata`: the same inputs must yield the same container
stats. This phase depends on nothing else and nothing depends on it.

**Moved in from the bridge:** the `sensors -j` parser and CPU temperature
mapping, the GPU sysfs and DRM readers, the physical-disk filter, the
filesystem usage listing, and the drive power-state reader.

**Dependencies:** `purego` is added for NVML behind the existing
`amd64 && glibc` build tags. `pflag`, `x/term`, `go-systemd` and modernc
sqlite are not added. The store moves to the repo's mattn cgo sqlite driver,
adapting its four error-code checks.

**Licensing:** the fork layer is relicensed Apache-2.0. `THIRD_PARTY_NOTICES`
gains a Beszel MIT section and a pointer to the go-monitoring repository for
the pre-import history.

## Build, release, packaging, docs

- Makefile: `build-monitoring` with `CGO_ENABLED=1` and the `glibc` tag on
  amd64, wired into `_build-binaries`, the Go target list, `clean` and help.
- `release.yml`: build step, `--version` verification, binary in the tarball,
  checksums and artifacts.
- Install scripts: binary and unit lists, `/etc/linuxio/monitoring`, enable the
  service. Uninstall mirrors it. No migration from a go-monitoring install.
- Docs: new `docs/monitoring.md` in the shape of the indexer guide; updates to
  `process-systemd-architecture.md` (binaries, units, build table, CLI),
  `capabilities.md`, README features and install text, this index, and
  `completed.md` when done.

## Testing

- Copied unit tests keep running after the import path rewrite. Tests of removed
  pieces go. Store tests adapt to mattn error values.
- New daemon tests: one-second reuse and concurrent sharing on a live key;
  stale-baseline reseed in the cpu, network, disk and docker managers; the
  `api.Live` builder from fixed inputs; strict YAML config loading, unknown-key
  rejection and live-apply of the collector and history fields; listener
  validation and per-listener plugin filtering of routes, `/api/v1/all`,
  `/api/v1/live` and the summary; the peer-uid gate on the control socket; the
  moved sensors parser, GPU readers, disk filter and filesystem listing against
  their existing fixtures; moby-client parity against the docker fixtures in
  the final phase.
- New bridge tests: `monitoring.get_live`, `monitoring.get_processes` and
  `monitoring.get_programs` against a fake daemon including the unreachable
  case; container metrics from the live block; capability detection; static
  route caching; CLI log filters.
- Frontend: updated query-ownership tests; card tests against the live payload;
  process page table, toggle, sort and filter; settings form for the new
  fields.
- Make targets: `make check-backend-quiet` and `make check-frontend-quiet`
  while iterating, `make generate` after schema edits, `make test-quiet` to
  finish since contracts change.
- Runtime on the development host after `make localinstall`: dashboard gauges as
  root and as a non-root login, Docker page, hardware page, storage page,
  history cards, settings, `linuxio logs monitoring`, and one configured TCP
  listener polled from a container.

## Follow-ups, not in this plan

- Read sensors from hwmon sysfs instead of `sensors -j`, removing a per-collection
  exec and the lm-sensors dependency at the cost of handling labels and scaling
  ourselves. Low priority; changes nothing visible.
- Process actions such as kill or signal, as bridge mutations.
