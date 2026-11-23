# Monitoring rework: Beszel's fixes, a smaller history store and cheaper scans

## Status

Not built. This is the agreed plan from the audit of 2026-11-22 and the
comparison with Beszel at commit `6702f01` (2026-11-21). What exists today is
in [Monitoring Daemon](../monitoring.md). The build starts when the owner says
so; step 1 is first.

## Problem

Every figure below is from the installed service (v0.56.0) on one host with 12
threads, 428 processes and 12 containers, on btrfs, unless it says harness.

- **The process scan is slow.** One scan takes 220 ms of CPU. It dominates the
  minute tick (280 ms), `/api/v1/all` (290 ms) and the Services tabs
  (`backend/monitoring/internal/collector/process/process.go`). gopsutil is
  called field by field, which is about 14 file reads per process where 5
  would do. Three of them are the whole-system `/proc/stat`, read again to
  work out the boot time, and each process also costs a user lookup.
- **Two thirds of the daemon's CPU goes to an outside tool.** Something polls
  the configured TCP listener every 30 seconds at about 300 ms a time, which
  matches `/api/v1/all`. The daemon uses 1.45% of one core in total.
- **The tick collects what nobody stores.** Connections (40 ms) and IRQs are
  gathered every minute and no history plugin keeps them
  (`backend/monitoring/internal/app/app.go`). The connection scan has its own
  10 second limit, and passing it fails the whole tick
  (`backend/monitoring/internal/collector/network/detail.go`).
- **The history store works harder than its data.** It keeps 43,200
  one-minute rows per table for 30 days and is 462 MB. A tick stores 4.9 KB
  of JSON and writes 340 KB, about 510 MB a day. The `resolution` column
  always holds `1m` and the `captured_at` index repeats the primary key
  (`backend/monitoring/internal/store/store.go`).
- **A long read loads the whole window.** A coarse resolution reads every
  one-minute row and averages the JSON in Go, decoding each row three times
  (`backend/monitoring/internal/plugins/aggregate.go`). 30 days of containers
  took 2.4 seconds, half the request limit, and six such reads raised the
  daemon's peak memory from 32 MB to 230 MB.
- **History is readable by any local user.** The bridge marks its six history
  routes privileged and reads them over `control.sock`, but the daemon mounts
  history on every listener (`backend/monitoring/internal/httpapi/plugin.go`),
  `api.sock` included.
- **A store version change stops the daemon.** A mismatch is a start error
  (`store.go`), so the unit restarts every five seconds for good. A corrupt
  database is moved aside and never deleted.
- **Drive health can be wrong.** A drive that can no longer be opened keeps
  its last "PASSED", because smartctl's exit status 2 is read as "asleep".
  One drive passing the 15 second limit ends the refresh for every drive
  after it, md arrays included. The status is derived from the temperature. A
  failed `smartctl --scan` on a host with md or eMMC deletes every drive
  record, and a swapped drive leaves the old record under the same device
  name (`backend/monitoring/internal/collector/smart/smart.go`).
- **A GPU collector stops for good on one line it cannot parse**, an Intel
  GPU reads zero between samples
  (`backend/monitoring/internal/collector/gpu/manager.go`), and a disk whose
  time counters wrap at 32 bits has that sample zeroed
  (`backend/monitoring/internal/collector/disk/disk.go`).
- **One stuck thing stalls the dashboard.** The live filesystem section calls
  `statfs` with no time limit inside the shared live collection
  (`backend/monitoring/internal/live/filesystems.go`), so a dead NFS server
  takes CPU and memory off the dashboard too.

## Decisions

**`api.sock` serves no history.** History is mounted on `control.sock`, which
the bridge already uses for it, and on configured listeners. SMART records,
the container list and per-process I/O stay readable on `api.sock`.

**A configured listener keeps serving everything.** One that names no plugins
serves all of them, history and process command lines included, without
authentication. Anything that can reach the address reads them, and so does a
local user through the loopback. The settings page says so beside the port
field.

**The outside API stays, and `/api/v1/all` gets cheap.** `/all`, the route per
plugin, the summary, connections and IRQs are kept for outside tools; LinuxIO
itself reads only `/live`, `/processes`, `/programs`, history and commands.
`/all` leaves out container telemetry, the part that needs a process scan,
unless the request asks for it.

**The four fixes Beszel already has are taken from Beszel.** Its SMART
collector was rewritten after the fork and covers every SMART defect above.
Its collector loop never stops for good, its "suspended GPU" shortcut does
not apply to a GPU that reports engines, and it computes the difference of a
wrapped disk time counter.

**A drive that stops answering is dropped, as in Beszel.** It leaves the
SMART list until it answers again. Beszel relies on its hub to keep the old
record; LinuxIO has no hub, so the page shows a missing drive and not a
warning.

**History is stored in four tiers**, Beszel's with its last two replaced by an
hourly one:

| Tier | Kept for | Rows per table |
| --- | --- | --- |
| 1m | 1 hour | 60 |
| 10m | 12 hours | 72 |
| 20m | 1 day | 72 |
| 60m | 30 days | 720 |

A rollup row is written when its bucket closes, from the rows of the tier
below, and a read returns stored rows of the tier it asks for. That is 924
rows per table against 43,200. The `resolution` column, which the fork kept
from Beszel without the rollups, gets its meaning back. `120m` and `480m` are
no longer offered.

Cockpit was read for comparison and has no tiers. Its metrics page reads the
PCP archives raw on a five second grid
([`metrics.jsx`](https://github.com/cockpit-project/cockpit/blob/main/pkg/metrics/metrics.jsx)).
pmlogger logs every 60 seconds unless told otherwise
([pmlogger(1)](https://man7.org/linux/man-pages/man1/pmlogger.1.html)) and its
daily job keeps 14 days of archives, compressed
([pmlogger_daily(1)](https://man7.org/linux/man-pages/man1/pmlogger_daily.1.html)).
So Cockpit keeps full detail for two weeks and nothing after.

**The old history is deleted on upgrade.** The first start with the new store
version deletes `metrics.db` and its sidecars and creates an empty store; the
charts start empty. A version the daemon does not know is handled the same
way, which ends the restart loop. A corrupt database is still moved aside,
and at most the newest such copy is kept.

**The daemon stays always on.** History needs a sample every minute and rates
need the previous sample in memory. It costs 27 MB of memory and half a
percent of one core. The two socket paths are already constants shared with
the bridge, so there is no path flag or setting to remove.

**The tick collects only what it stores.** Connections and IRQs are collected
when a request asks for them. The process scan runs on a tick only when
container telemetry history is due.

## Steps

Sizes are estimates and include tests and docs.

### Step 1: Beszel's four fixes

No new privileged or public surface. Backend only.

- **GPU collector loop.** A line the parser rejects is skipped, and a
  collector that ends is started again with a growing wait. `manager.go` and
  its test.
- **Intel GPU between samples.** The "suspended" shortcut applies only when
  the GPU reports no engines. One line and a test.
- **Disk time counters.** A lower value of a 32-bit millisecond counter is a
  wrap and not a reset. `disk.go` and its test.
- **SMART collector.** About 8 files: the collector, a parse file, the record
  types, the live map, the store's compact form, tests and
  `docs/monitoring.md`. It gets an independent review. Taken from Beszel:
  - standby is recognised from smartctl's message and not from exit status 2
    alone;
  - a failure or a timeout on one drive moves on to the next;
  - the status comes from the health field and is "UNKNOWN" when it is
    absent;
  - a failed scan keeps the previous device list;
  - the records are built again on each refresh, so a removed drive is
    forgotten and two devices with one serial are reported once;
  - the NVMe namespace is a second attempt on the same target;
  - temperature and the NVMe counters use wide types.

  Kept from LinuxIO: the caller's context, the typed self-test history, NVMe
  health details and power counters the storage page reads, and the md and
  eMMC readers.

### Step 2: history off `api.sock`

About 4 files, daemon only. It removes surface. The comment in
`backend/monitoring/api/routes.go` already says history is on `control.sock`
only; the sentence in `docs/monitoring.md` that justifies mode `0666` is
corrected to name what the socket serves beyond `/proc`.

### Step 3: cheaper scans

About 8 files, daemon only.

- gopsutil's boot time cache is switched on: one line, and about 1,280 fewer
  reads of `/proc/stat` per scan.
- The per-process gopsutil calls are replaced by a reader that opens each
  file once and looks a user name up once per uid. The estimate is 20 to 40
  ms per scan against 220; it is not measured in the daemon.
- Connections and IRQs leave the tick.
- `/api/v1/all` leaves out container telemetry unless asked.

### Step 4: store version 8

About 12 files: store, retention, rollups, averaging, the history contract,
the bridge's history adapter, the generated contract, the range choice on the
pages, tests and docs. It changes the bridge contract, so it needs
`make generate` and the full test run.

- The four tiers above, with the rollup written when a bucket closes.
- The retention delete runs once an hour and not on every tick.
- The `captured_at` index is dropped; the primary key covers every query.
- Averaging decodes each row once into its typed record.
- The version rule and the corrupt-copy rule under Decisions.

On upgrade the history is gone and the charts start empty.

### Step 5: live payload and collection fixes

Each is one or two files.

- The live `statfs` gets a time limit per mount, so a dead mount costs its
  own row and nothing else.
- The live filesystem list leaves out `binfmt_misc`, `efivarfs`, `hugetlbfs`,
  `mqueue` and `rpc_pipefs`; on the host above they are 5 of its 7 entries.
- CPU temperatures on a host with several sockets are named per chip, so one
  socket no longer overwrites another.
- The static part of the `nvidia-smi` answer is cached and not asked for on
  every live collection.
- The tick copies its container and filesystem records before it releases
  the lock, as the live path does, so a stored row cannot hold a live
  request's one-second value.
- A process counts for a container only when its cgroup path has the
  container's ID where Docker puts it, so a user's scope named after a
  container is not counted.
- `collector.interval` gets a lower bound.

## Measurements

CPU and time per request on the installed service:

| Request | CPU | Time |
| --- | --- | --- |
| `/api/v1/live` | 10 to 20 ms | 56 ms |
| `/api/v1/processes`, `programs`, `container_telemetry` | 220 ms | 200 ms |
| `/api/v1/all` | 290 ms | 210 ms |
| `/api/v1/connections` | 40 ms | 36 ms |
| One plugin (`cpu`, `mem`, `fs`) | 5 to 8 ms | 8 ms |
| 30 days of `containers` history at `480m` | 1,570 ms | 2.4 s |

Writes per tick in a harness that is not in the repository (the same schema
and row sizes, 30 days of rows, Python and the system SQLite). It reproduces
the real run: 222 KB to the write-ahead log per tick, written once more at
the checkpoint, against 340 KB observed.

| Variant | Pages per tick | Database |
| --- | --- | --- |
| Today | 55.6 | 329 MB |
| Today, retention delete hourly | 32.6 | 329 MB |
| No `resolution` column, no second index | 26.0 | 316 MB |
| Both | 14.2 | 316 MB |
| `WITHOUT ROWID` tables, both | not measured | 488 MB |

The four tiers are not in the harness. The host's database is 40% larger than
the harness predicts for its row sizes; the file could not be read without
root to see why.

## To settle

- **How a container counts in a rollup.** Today it is averaged over the
  samples where it existed and keyed by ID, so one minute at 100% CPU in a
  bucket reads as 100% and a container recreated by an image update becomes
  two series. Beszel keys by name and divides by every sample in the bucket.
  The owner comes back to this.
- **How long one-minute rows are kept.** With one hour, a look at last
  night's incident has 20 minute points, and hourly ones after a day. A day
  of one-minute rows is 1,440 rows per table, about 11 MB over all tables at
  today's row sizes; a week is about 77 MB (estimated from the harness, not
  measured).
- **Whether the tier lengths are settings**, and what `history.retention`
  names afterwards: the hourly tier is the natural reading.
- **How a request asks `/api/v1/all` for container telemetry.**
- **The wording of the warning beside a listener's port** in Settings.
- **The lower bound of `collector.interval`.** `1s` is accepted today and
  writes a row every second.
- **A configuration file the daemon cannot parse at start.** Today the daemon
  does not start, while a reload leaves the running configuration alone.
  Starting with the defaults would apply the default retention and delete
  history that the file meant to keep, so failing is the safer of the two.
  Unknown keys are refused, so a release that removes a key has to accept it
  still.
- **Configured unix-socket listeners.** They are mode `0660` and owned by
  root with no group setting, and the unit's sandbox leaves almost nowhere to
  put one. Removing them is about 4 files.
- **Socket units for the two fixed sockets.** The daemon binds them itself,
  so they do not exist until the first collection ends, and the bridge
  carries a retry loop for that. Units would be about 13 files and a check in
  the VM as installed and after a reboot.
- **smartctl versions.** Whether `--scan` and the `-a` output can name
  different types for one disk was not checked; the port follows Beszel's
  handling.

Inherited from Beszel, which still has each of them:

- **The network total counts stacked interfaces twice.** Only `lo`, `docker`,
  `br-`, `veth`, `bond` and `cali` names are skipped. On the host above the
  stored total is the physical NIC plus a VM's tap plus WireGuard. Which
  interfaces count has to be decided, and it changes what the stored total
  means from then on.
- **A container's status containing "second" wipes its baselines** on every
  pass, so it shows no CPU and no network for its first minute while a
  dashboard is open. The counter check a few lines below already covers a
  restart; whether the status check can go needs a test with a restarting
  container.
- **"bad memory stats"** leaves the container in the sample with zeros. The
  host's journal has it once for one container in 13 hours; a container
  leaving between the list and the stats call would explain it, and that is
  not confirmed.
- **Every failed container is retried**, and a stats pass has no limit of its
  own while it holds the collection lock.
- **Podman run by root is not found.** The socket is looked for under
  `/run/user/0`. Whether LinuxIO supports Podman at all is the first
  question; the Fedora VM can answer the rest.
- **GPU.** The NVIDIA library is called under the manager's lock, averages
  are shared between callers, a caller's first read is the mean since the
  daemon started, NVML memory is read only above 0% utilisation, nvtop gives
  up after one failure when it is the only collector, and an entry is never
  removed. These need a host with an NVIDIA GPU, as does a check of the size
  of the process list entry against the driver's header.
- **Sensors with the same name** are told apart by their position in the
  list, which changes when another sensor appears.
- **Disk.** A device's filesystem label can be picked as the root device, and
  the `statfs` of a tracked filesystem runs under the collection lock with no
  time limit.

## Not planned

- **Hiding SMART, the container list and per-process I/O from `api.sock`.** A
  session without sudo would lose the containers card and SMART on the
  dashboard. About 12 files.
- **A safe default set of plugins for a configured listener.** The owner's
  listener serves everything on purpose.
- **Trimming the outside API to what LinuxIO reads.** Outside tools use it.
- **Keeping a drive that stops answering on the page as "not responding".**
  Decided with the SMART port.
- **Migrating the old history.** It would be code that runs once.
- **A sampler started by a timer in place of the daemon.** It would lose the
  baselines between runs and open Docker and the GPU libraries every minute.
- **`WITHOUT ROWID` history tables.** The file grows to 488 MB in the
  harness.
- **Marking the monitoring folder no-CoW on btrfs.** The history cannot be
  rebuilt, so it keeps its checksums; the writes are cut instead.
