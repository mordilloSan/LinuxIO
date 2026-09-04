# linuxio-monitoring Plan 2: Bridge Boundary and Frontend Rewiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The bridge stops reading anything that changes over time. Every sampled measurement comes from `linuxio-monitoring` through one unprivileged route, `monitoring.get_live`; hardware identity stays on the bridge and is read once per session.

**Architecture:** The bridge collectors that sample (lm-sensors parsing, GPU sysfs and nvidia-smi readers, filesystem usage listing, NVMe power state, SMART reads, CPU frequencies) move into the daemon and feed new sections of `api.Live`. The daemon also keeps the raw smartctl JSON per device because the storage page renders smartctl's own structure. The bridge's system, network and storage routes shrink to identity data cached for the process lifetime. The frontend reads `monitoring.get_live` with `select` per card and requests static routes with a day-long stale time.

**Tech Stack:** Go 1.27, gopsutil, `lsblk`/`smartctl`/`nvme`/`sensors`/`nvidia-smi` subprocesses in the daemon, React 19 with TanStack Query and Router, vitest.

**Spec:** `docs/TODO/linuxio-monitoring.md`

**Depends on:** Plan 1 (`2026-09-03-linuxio-monitoring-1-daemon.md`) fully landed: `backend/monitoring/api`, `liveCurrentData`, `buildLive`, `monitoring.FetchLive`, `api.sock`.

## Global Constraints

- Everything in Plan 1's Global Constraints applies: no commits, Make targets only, no hand edits of generated files, `%w` wrapping, context-first signatures.
- Boundary rule: sampled measurements go through monitoring; hardware identity that needs a shutdown to change stays on the bridge, cached for the bridge process lifetime and requested by the frontend with `staleTime: CACHE_TTL_MS.ONE_DAY` and no `refetchInterval`; managed objects (containers, mounts, LVM, services, users, shares, network configuration, updates, health alerts) stay on the bridge as today.
- `monitoring.get_live` is unprivileged and `RetrySafe`. When the daemon is unreachable it returns a zero payload with empty maps and slices and `captured_at_ms: 0`; it never errors for that reason.
- Live sections map to plugins for listener allowlists: `filesystems` to `fs`, `sensors` to `sensors`, `gpus` to `gpu`, `smart` to `smart`, plus the Plan 1 mappings.
- Filesystem usage in the live payload is served from the daemon's disk-usage cache (`collector.disk_usage_cache`, default 0 which re-reads every collection). SMART data comes from the SMART cache on its refresh interval. Drive power state is read at most every 15 seconds.
- Extras readers (sensors exec, GPU sysfs, filesystems, SMART raw, power state, CPU frequencies) run outside the app mutex, once per live collection, only for `/api/v1/live` requests.
- `system.get_processes` stays untouched in this plan; Plan 3 replaces it.
- Frontend styling stays inside `components/ui` and `--app-*` variables; no new memoization outside `*Virtual*` files.

---

## File Structure

**Daemon, created**

| Path | Responsibility |
|---|---|
| `backend/monitoring/api/types.go` | Wire types moved from apischema: `SensorGroup`, `SensorReading`, `SensorReadingKind`, `FilesystemInfo`, `DiskPowerData`, `DiskPowerState`; new `LiveGPU`, `LiveSmart`. |
| `backend/monitoring/internal/app/live_extras.go` | `LiveExtras` collection outside the mutex; wiring into `liveCurrentData`. |
| `backend/monitoring/internal/app/sensors_lm.go` (+`_test.go`) | `sensors -j` parser and temperature classifier moved from the bridge. |
| `backend/monitoring/internal/app/filesystems.go` (+`_test.go`) | Per-mount usage listing with the disk-usage cache. |
| `backend/monitoring/internal/app/gpu_sysfs.go` (+`_test.go`) | DRM/sysfs/hwmon/nvidia-smi readers moved from the bridge, keyed by PCI address. |
| `backend/monitoring/internal/app/drive_power.go` (+`_test.go`) | NVMe power state with a 15-second cache, moved from the bridge. |
| `backend/monitoring/internal/app/cpu_freq.go` | Per-core current frequency reader moved from the bridge. |

**Daemon, modified**: `internal/app/smart.go` (raw output retention, `-x`), `internal/app/live_api.go` (`buildLive` extension), `internal/app/live_reuse.go` (`includeExtras`), `internal/domain/system/system.go` (`CombinedData.Extras`), `internal/api/http/server.go` (`handleLive` filters), `internal/app/disk.go` (`reseedFromCollector` unchanged; cache duration read).

**Bridge, modified**: `apischema/models.go`; `handlers/monitoring/{handlers.go,live.go,live_test.go}`; `handlers/system/{handlers.go,cpu.go,gpu.go,motherboard.go,hw_cache.go}`; `handlers/network/{handlers.go,network.go}`; `handlers/storage/{drives.go,smart_test_operation.go}`.

**Bridge, deleted**: `handlers/system/{memory.go,fs.go,disk_throughput.go,disk_throughput_test.go,sensors.go,sensors_test.go}`, `handlers/network/{interface_stats.go,interface_stats_test.go}`.

**Frontend, modified**: `api/operation-query-invalidations.ts`; `routes/_authenticated/index.tsx`; `-dashboard/{Processor,Memory,Network,Drive,FileSystem,SystemOverview,MotherBoard,Gpu}.tsx` and their tests; `hardware/route.tsx`, `hardware/-components/{HardwarePage,HardwareHistoryCards,hardwareQueryOptions}.tsx`; `components/cards/{SensorGroupCard,NetworkInterfaceCard,cardQueryOwnership.test}.tsx`; `network/-components/NetworkInterfaceList.tsx`; `storage/index.tsx`, `storage/-components/DiskOverview/index.tsx`; `routes/_authenticated/-query-ownership.test.ts`; `utils/gpu.ts`.

---

### Task 1: Move wire types and extend `api.Live`

**Files:**
- Create: `backend/monitoring/api/types.go`
- Modify: `backend/monitoring/api/live.go`, `backend/bridge/apischema/models.go`

**Interfaces:**
- Produces in `backend/monitoring/api`:

```go
type SensorReadingKind string

const (
	SensorReadingKindNumber  SensorReadingKind = "number"
	SensorReadingKindBoolean SensorReadingKind = "boolean"
)

type SensorReading struct {
	Field string            `json:"-"`
	Kind  SensorReadingKind `json:"kind"`
	Label string            `json:"label"`
	Unit  string            `json:"unit"`
	Value float64           `json:"value"`
}

type SensorGroup struct {
	Adapter  string          `json:"adapter"`
	Readings []SensorReading `json:"readings"`
}

type FilesystemInfo struct {
	Device            string   `json:"device"`
	Free              uint64   `json:"free"`
	FSType            string   `json:"fstype"`
	InodesFree        *uint64  `json:"inodesFree,omitempty"`
	InodesTotal       *uint64  `json:"inodesTotal,omitempty"`
	InodesUsed        *uint64  `json:"inodesUsed,omitempty"`
	InodesUsedPercent *float64 `json:"inodesUsedPercent,omitempty"`
	Mountpoint        string   `json:"mountpoint"`
	ReadOnly          *bool    `json:"readOnly,omitempty"`
	Total             uint64   `json:"total"`
	Used              uint64   `json:"used"`
	UsedPercent       float64  `json:"usedPercent"`
}

type DiskPowerState struct {
	Description string  `json:"description"`
	MaxPowerW   float64 `json:"maxPowerW"`
	State       int     `json:"state"`
}

type DiskPowerData struct {
	CurrentState int              `json:"currentState"`
	EstimatedW   float64          `json:"estimatedW"`
	States       []DiskPowerState `json:"states"`
}

// LiveGPU carries the fields that change while the machine runs, keyed by PCI
// address in Live.GPUs. Field names match the former GpuDevice JSON so the
// frontend merges them onto the static device by spreading.
type LiveGPU struct {
	ActualFreqMHz          *float64 `json:"actual_freq_mhz,omitempty"`
	BoostFreqMHz           *float64 `json:"boost_freq_mhz,omitempty"`
	ConnectedDisplays      *int     `json:"connected_displays,omitempty"`
	CurrentFreqMHz         *float64 `json:"current_freq_mhz,omitempty"`
	DisplayNames           []string `json:"display_names,omitempty"`
	DriverVersion          *string  `json:"driver_version,omitempty"`
	FanPercent             *float64 `json:"fan_percent,omitempty"`
	FanRPM                 *float64 `json:"fan_rpm,omitempty"`
	GTTTotalBytes          *uint64  `json:"gtt_total_bytes,omitempty"`
	GTTUsedBytes           *uint64  `json:"gtt_used_bytes,omitempty"`
	LinkSpeed              *string  `json:"link_speed,omitempty"`
	LinkWidth              *string  `json:"link_width,omitempty"`
	MaxFreqMHz             *float64 `json:"max_freq_mhz,omitempty"`
	MemoryFreeBytes        *uint64  `json:"memory_free_bytes,omitempty"`
	MemoryTotalBytes       *uint64  `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes        *uint64  `json:"memory_used_bytes,omitempty"`
	MinFreqMHz             *float64 `json:"min_freq_mhz,omitempty"`
	PowerDrawWatts         *float64 `json:"power_draw_watts,omitempty"`
	PowerLimitWatts        *float64 `json:"power_limit_watts,omitempty"`
	PowerState             *string  `json:"power_state,omitempty"`
	RC6ResidencyMS         *float64 `json:"rc6_residency_ms,omitempty"`
	RequestedFreqMHz       *float64 `json:"requested_freq_mhz,omitempty"`
	RP0FreqMHz             *float64 `json:"rp0_freq_mhz,omitempty"`
	RP1FreqMHz             *float64 `json:"rp1_freq_mhz,omitempty"`
	RPNFreqMHz             *float64 `json:"rpn_freq_mhz,omitempty"`
	RuntimeStatus          *string  `json:"runtime_status,omitempty"`
	TemperatureC           *float64 `json:"temperature_c,omitempty"`
	UtilizationPercent     *float64 `json:"utilization_percent,omitempty"`
	VisibleMemoryTotalByte *uint64  `json:"visible_memory_total_bytes,omitempty"`
	VisibleMemoryUsedBytes *uint64  `json:"visible_memory_used_bytes,omitempty"`
}

// LiveSmart is one drive's SMART view: smartctl's own JSON so the storage page
// keeps rendering smartctl's structure, plus NVMe power state.
type LiveSmart struct {
	Raw        json.RawMessage `json:"raw,omitempty"`
	Error      string          `json:"error,omitempty"`
	Power      *DiskPowerData  `json:"power,omitempty"`
	PowerError string          `json:"powerError,omitempty"`
}
```

- Extends `Live` with:

```go
	Filesystems             []FilesystemInfo     `json:"filesystems"`
	Sensors                 []SensorGroup        `json:"sensors"`
	GPUs                    map[string]LiveGPU   `json:"gpus"`
	Smart                   map[string]LiveSmart `json:"smart"`
	MotherboardTemperatures map[string]float64   `json:"motherboard_temperatures"`
```

and `LiveCPU` with `FrequenciesMHz []float64 `json:"frequencies_mhz"`` and `Temperatures map[string]float64 `json:"temperatures"`` (keys `package`, `coreN`).

- Produces in apischema: `type SensorReadingKind = monitoringapi.SensorReadingKind`, `type SensorReading = monitoringapi.SensorReading`, `type SensorGroup = monitoringapi.SensorGroup`, `type FilesystemInfo = monitoringapi.FilesystemInfo`, `type DiskPowerData = monitoringapi.DiskPowerData`, `type DiskPowerState = monitoringapi.DiskPowerState`, constants `SensorReadingKindNumber = monitoringapi.SensorReadingKindNumber` and `SensorReadingKindBoolean`, and `type MonitoringLive struct { monitoringapi.Live }`.

- [ ] **Step 1: Create `api/types.go`** with the block above and add the new fields to `Live` and `LiveCPU` in `api/live.go`. Add `"encoding/json"` to the imports.

- [ ] **Step 2: Alias in apischema**

In `models.go` delete the `SensorReadingKind` type and its two constants, `SensorReading`, `SensorGroup`, `FilesystemInfo`, `DiskPowerState`, `DiskPowerData`, and add:

```go
type (
	SensorReadingKind = monitoringapi.SensorReadingKind
	SensorReading     = monitoringapi.SensorReading
	SensorGroup       = monitoringapi.SensorGroup
	FilesystemInfo    = monitoringapi.FilesystemInfo
	DiskPowerData     = monitoringapi.DiskPowerData
	DiskPowerState    = monitoringapi.DiskPowerState
)

const (
	SensorReadingKindNumber  = monitoringapi.SensorReadingKindNumber
	SensorReadingKindBoolean = monitoringapi.SensorReadingKindBoolean
)

// MonitoringLive is the daemon's live payload, served unprivileged.
type MonitoringLive struct {
	monitoringapi.Live
}
```

The `EnumValues` entry `"SensorReadingKind": {"number", "boolean"}` stays.

- [ ] **Step 3: Compile**

```bash
make test-go GO_TEST_PKGS='./monitoring/api/... ./bridge/apischema/...' GO_TEST_FLAGS='-run XXX_NONE'
```

Expected: compiles. If the generator (`make generate`, run in Task 8) renders the aliased types under a different name, add them to `apischema.ExtraTypes` so the TypeScript names stay `SensorGroup`, `SensorReading`, `FilesystemInfo`, `DiskPowerData`, `DiskPowerState`.

- [ ] **Step 4: Stage**

```bash
git add backend/monitoring/api backend/bridge/apischema/models.go
```

---

### Task 2: Extras collection outside the mutex

**Files:**
- Create: `backend/monitoring/internal/app/live_extras.go`
- Modify: `backend/monitoring/internal/domain/system/system.go`, `backend/monitoring/internal/app/live_reuse.go`, `backend/monitoring/internal/app/live_reuse_test.go`, `backend/monitoring/internal/app/live_api.go`

**Interfaces:**
- Produces:

```go
// in domain/system
type LiveExtras struct {
	FrequenciesMHz          []float64
	CPUTemperatures         map[string]float64
	MotherboardTemperatures map[string]float64
	Sensors                 []monitoringapi.SensorGroup
	Filesystems             []monitoringapi.FilesystemInfo
	GPUs                    map[string]monitoringapi.LiveGPU
	Smart                   map[string]monitoringapi.LiveSmart
}
// CombinedData gains: Extras *LiveExtras `json:"-" cbor:"-"`

// in app
func (a *App) liveCurrentData(ctx context.Context, key uint16, includeDetails, includeContainers, includeExtras bool) (*system.CombinedData, time.Time, error)
func (a *App) collectLiveExtras(ctx context.Context) *system.LiveExtras // fills every section; each reader is independent and best-effort
```

- [ ] **Step 1: Write the failing test**

Append to `live_reuse_test.go`:

```go
func TestLiveCurrentDataCollectsExtrasOutsideCollection(t *testing.T) {
	a := &App{liveRuns: map[uint16]*liveRun{}}
	a.collectLive = func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, error) {
		return &system.CombinedData{}, nil
	}
	calls := 0
	a.collectExtras = func(ctx context.Context) *system.LiveExtras {
		calls++
		return &system.LiveExtras{FrequenciesMHz: []float64{1200}}
	}
	data, _, err := a.liveCurrentData(context.Background(), 1002, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if data.Extras == nil || data.Extras.FrequenciesMHz[0] != 1200 || calls != 1 {
		t.Fatalf("extras not attached: %+v calls=%d", data.Extras, calls)
	}
	plain, _, err := a.liveCurrentData(context.Background(), 1010, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Extras != nil || calls != 1 {
		t.Fatal("plain live keys must not collect extras")
	}
	if _, _, err := a.liveCurrentData(context.Background(), 1010, false, false, true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("a sample without extras must not satisfy a request with extras, calls=%d", calls)
	}
}
```

Update the Plan 1 tests in this file to pass the new fifth argument (`false`).

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run TestLiveCurrentData'
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement**

`domain/system/system.go`: add the `LiveExtras` type (import `monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"`) and the `Extras *LiveExtras `json:"-" cbor:"-"`` field on `CombinedData`.

`live_reuse.go`: add `includeExtras bool` to `liveRun`, extend `covers` with `(r.includeExtras || !includeExtras)`, add the parameter to `liveCurrentData`, and after `run.data, run.err = collect(...)` add:

```go
		if run.err == nil && includeExtras {
			extras := a.collectExtras
			if extras == nil {
				extras = a.collectLiveExtras
			}
			run.data.Extras = extras(ctx)
		}
```

Add `collectExtras func(ctx context.Context) *system.LiveExtras` to `App` beside `collectLive`. Update the Plan 1 callers: `collectSystemPlugin`, `collectCurrentSystemBatch`, `SystemSummary` pass `false`; `Live()` passes `true`.

`live_extras.go`:

```go
package app

import (
	"context"
	"log/slog"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// collectLiveExtras gathers the LinuxIO-only sections that read sysfs or run
// helper binaries. It runs after the mutex-guarded sample so slow readers never
// block the collector tick. Every reader is best effort and independent.
func (a *App) collectLiveExtras(ctx context.Context) *system.LiveExtras {
	extras := &system.LiveExtras{}
	var wg sync.WaitGroup
	wg.Go(func() { extras.FrequenciesMHz = readCPUFrequencies(ctx) })
	wg.Go(func() {
		groups := collectLMSensors(ctx)
		cpu, board := classifyTemperatures(groups)
		extras.Sensors, extras.CPUTemperatures, extras.MotherboardTemperatures = groups, cpu, board
	})
	wg.Go(func() {
		filesystems, err := a.fsManager.linuxioFilesystems(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Debug("filesystem listing failed", "err", err)
		}
		extras.Filesystems = filesystems
	})
	wg.Go(func() { extras.GPUs = collectLiveGPUs(ctx) })
	wg.Go(func() { extras.Smart = a.liveSmart(ctx) })
	wg.Wait()
	return extras
}
```

The four readers are defined in Tasks 3 to 6; until they land, stub them as functions returning empty values in the files that will own them so this task compiles, then replace in each task.

- [ ] **Step 4: Run**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run TestLiveCurrentData'
```

Expected: PASS.

- [ ] **Step 5: Stage**

```bash
git add backend/monitoring/internal
```

---

### Task 3: Sensors parser and temperature classifier move into the daemon

**Files:**
- Create: `backend/monitoring/internal/app/sensors_lm.go`, `backend/monitoring/internal/app/sensors_lm_test.go`
- Source: `backend/bridge/handlers/system/sensors.go`, `sensors_test.go` (deleted in Task 7)

**Interfaces:**
- Produces: `func collectLMSensors(ctx context.Context) []monitoringapi.SensorGroup` (nil when `sensors` is absent or fails) and `func classifyTemperatures(groups []monitoringapi.SensorGroup) (cpu, board map[string]float64)` where `cpu` holds `package` and `coreN`, `board` holds `mbN` and `driveN`.

- [ ] **Step 1: Move the code**

```bash
cd /home/miguelmariz/LinuxIO
cp backend/bridge/handlers/system/sensors.go backend/monitoring/internal/app/sensors_lm.go
cp backend/bridge/handlers/system/sensors_test.go backend/monitoring/internal/app/sensors_lm_test.go
sed -i 's/^package system$/package app/' backend/monitoring/internal/app/sensors_lm.go backend/monitoring/internal/app/sensors_lm_test.go
sed -i 's#"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"#monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"#; s/apischema\./monitoringapi./g' backend/monitoring/internal/app/sensors_lm.go backend/monitoring/internal/app/sensors_lm_test.go
```

Rename inside `sensors_lm.go`: `FetchSensorsInfo` to `collectLMSensors`; `getTemperatureMap(ctx)` to:

```go
// classifyTemperatures splits lm-sensors readings into the CPU keys the
// processor card shows (package, coreN) and everything else (mbN, driveN).
func classifyTemperatures(groups []monitoringapi.SensorGroup) (cpu, board map[string]float64) {
	cpu, board = map[string]float64{}, map[string]float64{}
	indices := temperatureIndexes{}
	for _, group := range groups {
		adapter := strings.ToLower(group.Adapter)
		for _, r := range group.Readings {
			value, ok := sensorNumberValue(r)
			if !ok || !sensorReadingIsInput(r) {
				continue
			}
			key, ok := classifyTemperatureReading(adapter, r, &indices)
			if !ok {
				continue
			}
			if key == "package" || strings.HasPrefix(key, "core") {
				cpu[key] = value
			} else {
				board[key] = value
			}
		}
	}
	return cpu, board
}
```

Keep `sensorsCommand = exec.CommandContext` as the test seam. Add a `sensorsPath` lookup cached with `sync.OnceValues(func() (string, error) { return exec.LookPath("sensors") })` and return nil early when it errs, so hosts without lm-sensors never fork.

- [ ] **Step 2: Adapt the moved tests**

The bridge test used `getTemperatureMap`; rewrite those assertions against `classifyTemperatures` (CPU keys land in the first map, board and drive keys in the second). Keep the parser tests untouched.

- [ ] **Step 3: Run**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run "Sensor|Temperature"'
```

Expected: PASS.

- [ ] **Step 4: Stage**

```bash
git add backend/monitoring/internal/app/sensors_lm.go backend/monitoring/internal/app/sensors_lm_test.go
```

---

### Task 4: Filesystem listing moves into the daemon

**Files:**
- Create: `backend/monitoring/internal/app/filesystems.go`, `backend/monitoring/internal/app/filesystems_test.go`
- Modify: `backend/monitoring/internal/app/disk.go` (fields)

**Interfaces:**
- Produces: `func (m *fsManager) linuxioFilesystems(ctx context.Context) ([]monitoringapi.FilesystemInfo, error)`; results are cached for `m.diskUsageCacheDuration` (0 disables caching).

- [ ] **Step 1: Write the failing test**

```go
package app

import (
	"context"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestLinuxioFilesystemsHonoursUsageCache(t *testing.T) {
	m := newFsManager()
	m.setDiskUsageCache(time.Minute)
	calls := 0
	m.listPartitions = func(context.Context) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{
			{Device: "/dev/sda1", Mountpoint: "/", Fstype: "ext4", Opts: []string{"rw"}},
			{Device: "proc", Mountpoint: "/proc", Fstype: "proc"},
		}, nil
	}
	m.usageOf = func(context.Context, string) (*disk.UsageStat, error) {
		calls++
		return &disk.UsageStat{Total: 100, Used: 40, Free: 60, UsedPercent: 40}, nil
	}
	first, err := m.linuxioFilesystems(context.Background())
	if err != nil || len(first) != 1 || first[0].Mountpoint != "/" || first[0].Used != 40 {
		t.Fatalf("first = %+v err=%v", first, err)
	}
	if _, err := m.linuxioFilesystems(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("usage read %d times inside the cache window, want 1", calls)
	}
	m.setDiskUsageCache(0)
	if _, err := m.linuxioFilesystems(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("cache disabled must re-read, calls=%d", calls)
	}
}
```

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run TestLinuxioFilesystemsHonoursUsageCache'
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement**

`filesystems.go`:

```go
package app

import (
	"context"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

// linuxioFilesystems lists real mounts with usage for the live payload. The
// result is reused for diskUsageCacheDuration so sleeping disks stay asleep
// when the operator asks for that; zero re-reads on every call.
func (m *fsManager) linuxioFilesystems(ctx context.Context) ([]monitoringapi.FilesystemInfo, error) {
	m.linuxioFsMu.Lock()
	defer m.linuxioFsMu.Unlock()
	if m.diskUsageCacheDuration > 0 && m.linuxioFs != nil && time.Since(m.linuxioFsAt) < m.diskUsageCacheDuration {
		return m.linuxioFs, nil
	}
	listPartitions := m.listPartitions
	if listPartitions == nil {
		listPartitions = func(ctx context.Context) ([]disk.PartitionStat, error) { return disk.PartitionsWithContext(ctx, true) }
	}
	usageOf := m.usageOf
	if usageOf == nil {
		usageOf = disk.UsageWithContext
	}
	parts, err := listPartitions(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]monitoringapi.FilesystemInfo, 0, len(parts))
	for _, p := range parts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if isPseudoFS(p) {
			continue
		}
		usage, err := usageOf(ctx, p.Mountpoint)
		if err != nil {
			continue
		}
		results = append(results, monitoringapi.FilesystemInfo{
			Device:            p.Device,
			Mountpoint:        p.Mountpoint,
			FSType:            p.Fstype,
			ReadOnly:          new(utils.HasReadOnlyOpt(p.Opts)),
			Total:             usage.Total,
			Used:              usage.Used,
			Free:              usage.Free,
			UsedPercent:       usage.UsedPercent,
			InodesTotal:       utils.OptionalUint64(usage.InodesTotal),
			InodesUsed:        utils.OptionalUint64(usage.InodesUsed),
			InodesFree:        utils.OptionalUint64(usage.InodesFree),
			InodesUsedPercent: utils.OptionalFloat64(usage.InodesUsedPercent),
		})
	}
	m.linuxioFs, m.linuxioFsAt = results, time.Now()
	return results, nil
}

func isPseudoFS(p disk.PartitionStat) bool {
	if strings.HasPrefix(p.Device, "/dev/") {
		return false
	}
	switch p.Fstype {
	case "proc", "sysfs", "devtmpfs", "devpts", "tmpfs", "cgroup", "cgroup2", "pstore",
		"securityfs", "debugfs", "tracefs", "configfs", "overlay", "squashfs", "ramfs",
		"bpf", "nsfs", "autofs", "fusectl":
		return true
	}
	return false
}
```

Add to `fsManager` in `disk.go`:

```go
	linuxioFsMu    sync.Mutex
	linuxioFs      []monitoringapi.FilesystemInfo
	linuxioFsAt    time.Time
	listPartitions func(context.Context) ([]disk.PartitionStat, error) // nil in production; tests inject
	usageOf        func(context.Context, string) (*disk.UsageStat, error)
```

If `setDiskUsageCache` from Plan 1 is guarded by the app mutex, read `diskUsageCacheDuration` through a small getter under `linuxioFsMu` instead; the point is that this method never takes the app mutex.

- [ ] **Step 4: Run**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run TestLinuxioFilesystems'
```

Expected: PASS.

- [ ] **Step 5: Stage**

```bash
git add backend/monitoring/internal/app
```

---

### Task 5: GPU live readers move into the daemon

**Files:**
- Create: `backend/monitoring/internal/app/gpu_sysfs.go`, `backend/monitoring/internal/app/gpu_sysfs_test.go`
- Source: `backend/bridge/handlers/system/gpu.go` lines for `enrichGPUFromSysfs`, `findGPUCardDir`, `enrichGPUFromHwmon`, `enrichConnectedDisplays`, `readGPUUtilization`, `estimateBusyPercentFromRC6`, `readNvidiaSMIStats`, `mergeNvidiaStats`, `addMemoryMetric`, `normalizePCIAddress`, the `set*`/`get*`/`readSysfs*`/`readlink`/`sanitize*`/`parseOptional*`/`firstExistingPath`/`pathIsDir`/`firstLine`/`round1` helpers and `nvidiaGPUStats`

**Interfaces:**
- Produces: `func collectLiveGPUs(ctx context.Context) map[string]monitoringapi.LiveGPU`, keyed by normalized PCI address (`0000:01:00.0`).

- [ ] **Step 1: Move the readers**

Copy the listed functions verbatim into `gpu_sysfs.go` with `package app`. Replace the bridge-only entry points with:

```go
// collectLiveGPUs walks /sys/class/drm, reads each card's PCI device, and
// returns the mutable fields per PCI address. Static identity stays with the
// bridge, which joins on the same address.
func collectLiveGPUs(ctx context.Context) map[string]monitoringapi.LiveGPU {
	out := map[string]monitoringapi.LiveGPU{}
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return out
	}
	nvidiaStats := readNvidiaSMIStats(ctx)
	for _, e := range entries {
		if ctx.Err() != nil {
			return out
		}
		if !strings.HasPrefix(e.Name(), "card") || strings.Contains(e.Name(), "-") {
			continue
		}
		pciAddr := normalizePCIAddress(filepath.Base(readlink(filepath.Join("/sys/class/drm", e.Name(), "device"))))
		if pciAddr == "" {
			continue
		}
		entry := map[string]any{}
		enrichGPUFromSysfs(pciAddr, entry)
		if stats, ok := nvidiaStats[pciAddr]; ok {
			mergeNvidiaStats(entry, stats)
		}
		delete(entry, "drm_card")
		delete(entry, "driver_module")
		delete(entry, "raw_class")
		delete(entry, "boot_vga")
		delete(entry, "max_link_speed")
		delete(entry, "max_link_width")
		live, err := liveGPUFromEntry(entry)
		if err != nil {
			continue
		}
		out[pciAddr] = live
	}
	return out
}

func liveGPUFromEntry(entry map[string]any) (monitoringapi.LiveGPU, error) {
	var live monitoringapi.LiveGPU
	data, err := json.Marshal(entry)
	if err != nil {
		return live, err
	}
	return live, json.Unmarshal(data, &live)
}
```

The deleted keys are the static ones the bridge keeps (Task 7). `estimateBusyPercentFromRC6` sleeps 120 ms; it now runs outside the app mutex and at most once per second, so leave it as is.

- [ ] **Step 2: Test the entry mapping and the static-key stripping**

```go
package app

import "testing"

func TestLiveGPUFromEntryMapsSnakeKeys(t *testing.T) {
	live, err := liveGPUFromEntry(map[string]any{
		"utilization_percent": 42.5,
		"temperature_c":       61.0,
		"memory_used_bytes":   uint64(1024),
		"runtime_status":      "active",
		"connected_displays":  2,
		"display_names":       []string{"HDMI-A-1 (1920x1080)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if *live.UtilizationPercent != 42.5 || *live.TemperatureC != 61 || *live.MemoryUsedBytes != 1024 || *live.RuntimeStatus != "active" || *live.ConnectedDisplays != 2 || live.DisplayNames[0] != "HDMI-A-1 (1920x1080)" {
		t.Fatalf("live = %+v", live)
	}
	if live.FanRPM != nil {
		t.Fatal("absent keys must stay nil")
	}
}
```

- [ ] **Step 3: Run**

```bash
make test-go GO_TEST_PKGS=./monitoring/internal/app/... GO_TEST_FLAGS='-run TestLiveGPUFromEntry'
```

Expected: PASS.

- [ ] **Step 4: Stage**

```bash
git add backend/monitoring/internal/app/gpu_sysfs.go backend/monitoring/internal/app/gpu_sysfs_test.go
```

---

### Task 6: SMART raw retention, NVMe power state, CPU frequencies, builder extension

**Files:**
- Create: `backend/monitoring/internal/app/drive_power.go`, `backend/monitoring/internal/app/drive_power_test.go`, `backend/monitoring/internal/app/cpu_freq.go`, `backend/monitoring/internal/app/smart_live.go`
- Modify: `backend/monitoring/internal/app/smart.go`, `backend/monitoring/internal/app/live_api.go`, `backend/monitoring/internal/app/live_api_test.go`, `backend/monitoring/internal/api/http/server.go`, `backend/monitoring/internal/api/http/server_test.go`
- Source: `backend/bridge/handlers/storage/drives.go` (`GetNVMePowerState`, `resolveCurrentNVMePowerState`, `nvmePsRe`, `nvmeStateRe`), `backend/bridge/handlers/system/cpu.go` (`getCurrentFrequencies`)

**Interfaces:**
- Produces:
  - `func (sm *SmartManager) RawOutputs() map[string]json.RawMessage` keyed by short device name (`sda`, `nvme0n1`); a controller path `/dev/nvme0` is also recorded under `nvme0n1`.
  - `func (a *App) liveSmart(ctx context.Context) map[string]monitoringapi.LiveSmart`.
  - `type drivePowerManager struct` with `func (m *drivePowerManager) get(ctx context.Context, device string) (*monitoringapi.DiskPowerData, error)` cached for `drivePowerCacheTTL = 15 * time.Second`.
  - `func readCPUFrequencies(ctx context.Context) []float64`.
  - `buildLive` keeps its Plan 1 signature and reads `data.Extras` (nil-safe) to fill `Filesystems`, `Sensors`, `GPUs`, `Smart`, `MotherboardTemperatures`, `CPU.FrequenciesMHz`, `CPU.Temperatures`.

- [ ] **Step 1: Retain raw smartctl output**

In `smart.go` add `rawOutput map[string]json.RawMessage` to `SmartManager` (initialised in `NewSmartManager`). In `parseSmartOutput`, when parsing succeeds, store a copy under both names:

```go
	if hasValidData {
		sm.Lock()
		sm.rawOutput[filepath.Base(deviceInfo.Name)] = json.RawMessage(bytes.Clone(output))
		if isNvmeControllerPath(deviceInfo.Name) {
			sm.rawOutput[filepath.Base(deviceInfo.Name)+"n1"] = json.RawMessage(bytes.Clone(output))
		}
		sm.Unlock()
	}
```

Change `smartctlArgs` to request the full report the storage page renders: replace `"-a", "--json=c"` with `"-x", "--json=c"` (the `-l devstat` addition for ATA is then redundant; keep it, smartctl accepts both). Add:

```go
// RawOutputs returns the last successful smartctl JSON per device.
func (sm *SmartManager) RawOutputs() map[string]json.RawMessage {
	sm.Lock()
	defer sm.Unlock()
	return maps.Clone(sm.rawOutput)
}
```

Adjust any smart test that asserts the exact argument list.

- [ ] **Step 2: Move the power-state reader**

`drive_power.go`:

```go
package app

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const drivePowerCacheTTL = 15 * time.Second

var (
	nvmePsRe    = regexp.MustCompile(`ps\s+(\d+)\s+:\s+mp:([\d.]+)W`)
	nvmeStateRe = regexp.MustCompile(`Power State:\s+(\d+)`)
)

type drivePowerEntry struct {
	at    time.Time
	data  *monitoringapi.DiskPowerData
	err   error
}

// drivePowerManager caches NVMe power state per device so the live payload
// forks nvme at most every drivePowerCacheTTL per drive.
type drivePowerManager struct {
	mu      sync.Mutex
	entries map[string]drivePowerEntry
	run     func(ctx context.Context, name string, args ...string) ([]byte, error) // nil means exec.CommandContext
}

func (m *drivePowerManager) get(ctx context.Context, device string) (*monitoringapi.DiskPowerData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = map[string]drivePowerEntry{}
	}
	if entry, ok := m.entries[device]; ok && time.Since(entry.at) < drivePowerCacheTTL {
		return entry.data, entry.err
	}
	data, err := m.read(ctx, device)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.entries[device] = drivePowerEntry{at: time.Now(), data: data, err: err}
	return data, err
}

func (m *drivePowerManager) output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.run != nil {
		return m.run(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).Output()
}

func (m *drivePowerManager) read(ctx context.Context, device string) (*monitoringapi.DiskPowerData, error) {
	out, err := m.output(ctx, "nvme", "id-ctrl", "/dev/"+device)
	if err != nil {
		return nil, fmt.Errorf("nvme id-ctrl %s: %w", device, err)
	}
	var states []monitoringapi.DiskPowerState
	for line := range strings.SplitSeq(string(out), "\n") {
		match := nvmePsRe.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		stateNum, parseErr := strconv.Atoi(match[1])
		if parseErr != nil {
			continue
		}
		maxPower, powerErr := strconv.ParseFloat(match[2], 64)
		if powerErr != nil {
			continue
		}
		states = append(states, monitoringapi.DiskPowerState{State: stateNum, MaxPowerW: maxPower, Description: strings.TrimSpace(line)})
	}
	if len(states) == 0 {
		return nil, fmt.Errorf("no power states found for %s", device)
	}
	current, estimated := -1, states[0].MaxPowerW
	if log, err := m.output(ctx, "nvme", "smart-log", "/dev/"+device); err == nil {
		if match := nvmeStateRe.FindStringSubmatch(string(log)); len(match) == 2 {
			if s, err := strconv.Atoi(match[1]); err == nil {
				current, estimated = s, 0
				for _, ps := range states {
					if ps.State == s {
						estimated = ps.MaxPowerW
					}
				}
			}
		}
	}
	return &monitoringapi.DiskPowerData{CurrentState: current, EstimatedW: estimated, States: states}, nil
}
```

`drive_power_test.go`:

```go
package app

import (
	"context"
	"testing"
)

func TestDrivePowerParsesStatesAndCaches(t *testing.T) {
	calls := 0
	m := &drivePowerManager{run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls++
		if args[0] == "id-ctrl" {
			return []byte("ps    0 : mp:8.00W operational\nps    1 : mp:4.00W operational\n"), nil
		}
		return []byte("Power State:                        1\n"), nil
	}}
	data, err := m.get(context.Background(), "nvme0n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.States) != 2 || data.CurrentState != 1 || data.EstimatedW != 4 {
		t.Fatalf("data = %+v", data)
	}
	if _, err := m.get(context.Background(), "nvme0n1"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("second get must hit the cache, calls=%d", calls)
	}
}
```

- [ ] **Step 3: Live SMART assembly**

`smart_live.go`:

```go
package app

import (
	"context"
	"strings"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

// liveSmart pairs the cached smartctl JSON with NVMe power state per drive.
func (a *App) liveSmart(ctx context.Context) map[string]monitoringapi.LiveSmart {
	out := map[string]monitoringapi.LiveSmart{}
	if a.smartManager == nil {
		return out
	}
	for name, raw := range a.smartManager.RawOutputs() {
		if ctx.Err() != nil {
			return out
		}
		entry := monitoringapi.LiveSmart{Raw: raw}
		if strings.HasPrefix(name, "nvme") {
			power, err := a.drivePower.get(ctx, name)
			if err != nil {
				entry.PowerError = err.Error()
			} else {
				entry.Power = power
			}
		}
		out[name] = entry
	}
	return out
}
```

Add `drivePower drivePowerManager` to `App`.

- [ ] **Step 4: CPU frequencies**

`cpu_freq.go`: move `getCurrentFrequencies` from the bridge, renamed `readCPUFrequencies(ctx context.Context) []float64`, returning `nil` on error instead of `(nil, err)`.

- [ ] **Step 5: Extend the builder and its test**

In `live_api.go` change the signature to `buildLive(data *system.CombinedData, capturedAt time.Time, threads int, telemetry []container.Telemetry, telemetryAt time.Time, freshFor time.Duration) monitoringapi.Live` reading `data.Extras`, and add after the interfaces loop:

```go
	live.Filesystems = []monitoringapi.FilesystemInfo{}
	live.Sensors = []monitoringapi.SensorGroup{}
	live.GPUs = map[string]monitoringapi.LiveGPU{}
	live.Smart = map[string]monitoringapi.LiveSmart{}
	live.MotherboardTemperatures = map[string]float64{}
	live.CPU.FrequenciesMHz = []float64{}
	live.CPU.Temperatures = map[string]float64{}
	if extras := data.Extras; extras != nil {
		if extras.Filesystems != nil {
			live.Filesystems = extras.Filesystems
		}
		if extras.Sensors != nil {
			live.Sensors = extras.Sensors
		}
		if extras.GPUs != nil {
			live.GPUs = extras.GPUs
		}
		if extras.Smart != nil {
			live.Smart = extras.Smart
		}
		if extras.MotherboardTemperatures != nil {
			live.MotherboardTemperatures = extras.MotherboardTemperatures
		}
		if extras.FrequenciesMHz != nil {
			live.CPU.FrequenciesMHz = extras.FrequenciesMHz
		}
		if extras.CPUTemperatures != nil {
			live.CPU.Temperatures = extras.CPUTemperatures
		}
	}
```

Extend `TestBuildLiveMapsSample` with `data.Extras = &system.LiveExtras{CPUTemperatures: map[string]float64{"package": 55}, Filesystems: []monitoringapi.FilesystemInfo{{Mountpoint: "/", Total: 10}}}` and assert `live.CPU.Temperatures["package"] == 55` and `live.Filesystems[0].Mountpoint == "/"`; add a case with `Extras == nil` asserting the empty, non-nil sections.

- [ ] **Step 6: Filter the new sections on restricted listeners**

In `server.go` extend `liveSectionPlugins` with `"filesystems": {"fs"}`, `"sensors": {"sensors"}`, `"gpus": {"gpu"}`, `"smart": {"smart"}`, and in `handleLive` zero them when not permitted (`live.Filesystems = []monitoringapi.FilesystemInfo{}` and so on; `MotherboardTemperatures` and `CPU.Temperatures` follow `sensors`; `CPU.FrequenciesMHz` follows `cpu`). Extend `TestLiveRouteFiltersSectionsByAllowlist` with a `sensors` payload that must be emptied under `[]string{"cpu"}`.

- [ ] **Step 7: Run every daemon package**

```bash
make test-go GO_TEST_PKGS=./monitoring/...
```

Expected: PASS.

- [ ] **Step 8: Stage**

```bash
git add backend/monitoring
```

---

### Task 7: Bridge routes shrink to identity; `monitoring.get_live`

**Files:**
- Modify: `backend/bridge/apischema/models.go`, `backend/bridge/handlers/monitoring/handlers.go`, `live.go`, `live_test.go`, `backend/bridge/handlers/system/handlers.go`, `cpu.go`, `gpu.go`, `motherboard.go`, `motherboard_test.go`, `hw_cache.go`, `backend/bridge/handlers/network/handlers.go`, `network.go`, `network_test.go`, `backend/bridge/handlers/storage/drives.go`, `smart_test_operation.go`
- Create: `backend/bridge/handlers/system/cpu_test.go`
- Delete: `backend/bridge/handlers/system/{memory.go,fs.go,disk_throughput.go,disk_throughput_test.go,sensors.go,sensors_test.go}`, `backend/bridge/handlers/network/{interface_stats.go,interface_stats_test.go}`

**Interfaces:**
- Produces:
  - Route `monitoring.get_live` (`apischema.NoRequest` to `apischema.MonitoringLive`, `RetrySafe`, unprivileged).
  - `monitoring.RefreshSmart(ctx context.Context) error` sending `smart.refresh` on the control socket.
  - `apischema.CPUInfoResponse{Cores int; Family, Model, ModelName, VendorID string; MHz float64}`.
  - `apischema.MotherboardInfo{Baseboard, BIOS}` (no temperatures).
  - `apischema.GpuDevice` static fields only: `Address, DeviceID, Driver, Model string; Revision, Subsystem, SubsystemID, Vendor, VendorID string; BootVGA *bool; ClassName, DriverModule, DRMCard, MaxLinkSpeed, MaxLinkWidth, ProgrammingInterface, RawClass, SubclassName *string; NUMANode *int`.
  - `apischema.ApiDisk{Model, Name, Size string; RO bool; Serial, Type, Vendor *string}`.
  - `apischema.NetworkInterface` without `RXSpeed` and `TXSpeed`.
  - Removed routes: `system.get_memory_info`, `system.get_disk_throughput`, `system.get_uptime`, `system.get_fs_info`, `system.get_sensor_info`, `network.get_interface_stats`. Removed types: `MemoryInfoResponse` and its parts, `DiskThroughputResponse`, `DiskThroughputDevice`, `InterfaceStats`, `MotherboardTemperatures`.

- [ ] **Step 1: Write the failing live handler test**

Append to `handlers/monitoring/live_test.go`:

```go
func TestHandleGetLiveReturnsZeroPayloadWhenUnavailable(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) { return nil, syscall.ECONNREFUSED })
	live, err := handleGetLive(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatalf("unavailable daemon must not error: %v", err)
	}
	if live.CapturedAtMs != 0 || live.Disks == nil || live.Interfaces == nil || live.Filesystems == nil || live.Sensors == nil || live.GPUs == nil || live.Smart == nil || live.Containers.Items == nil || live.CPU.PerCorePercent == nil {
		t.Fatalf("zero payload must carry empty collections: %+v", live)
	}
}

func TestRefreshSmartSendsCommand(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		if cmd := decodeCommandRequest(t, req); cmd.Command != "smart.refresh" {
			t.Fatalf("command = %q", cmd.Command)
		}
		return jsonResponse(http.StatusOK, `{"ok": true, "command": "smart.refresh", "data": {"refreshed": true}}`), nil
	})
	if err := RefreshSmart(context.Background()); err != nil {
		t.Fatal(err)
	}
}
```

Add the `apischema` import. `withTestMonitoringClient` from `config_test.go` must swap `controlClient`'s transport after Plan 1; confirm it does.

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./bridge/handlers/monitoring/... GO_TEST_FLAGS='-run "TestHandleGetLive|TestRefreshSmart"'
```

Expected: FAIL to compile.

- [ ] **Step 3: Add the route and helpers**

In `handlers.go` add to the bindings:

```go
	apischema.Call[apischema.NoRequest, apischema.MonitoringLive]("monitoring.get_live", apischema.RetrySafe()).Handle(handleGetLive),
```

and:

```go
func handleGetLive(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringLive, error) {
	live, err := FetchLive(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return apischema.MonitoringLive{}, ctx.Err()
		}
		return apischema.MonitoringLive{Live: emptyLive()}, nil
	}
	return apischema.MonitoringLive{Live: live}, nil
}
```

In `live.go`:

```go
// emptyLive is what unprivileged callers see while the daemon is down: zero
// values with every collection present, so cards render empty rather than fail.
func emptyLive() monitoringapi.Live {
	return monitoringapi.Live{
		CPU:                     monitoringapi.LiveCPU{PerCorePercent: []float64{}, FrequenciesMHz: []float64{}, Temperatures: map[string]float64{}},
		Disks:                   map[string]monitoringapi.LiveDiskRates{},
		Interfaces:              map[string]monitoringapi.LiveInterface{},
		Filesystems:             []monitoringapi.FilesystemInfo{},
		Sensors:                 []monitoringapi.SensorGroup{},
		GPUs:                    map[string]monitoringapi.LiveGPU{},
		Smart:                   map[string]monitoringapi.LiveSmart{},
		MotherboardTemperatures: map[string]float64{},
		Containers:              monitoringapi.LiveContainers{Items: []monitoringapi.LiveContainer{}},
	}
}

// RefreshSmart asks the daemon to re-read SMART now, for example after a
// self-test finishes. Privileged callers only, over the control socket.
func RefreshSmart(ctx context.Context) error {
	if _, err := runCommand(ctx, "smart.refresh", nil); err != nil {
		return fmt.Errorf("refresh smart: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Shrink the system package**

`apischema/models.go`: rewrite `CPUInfoResponse`, `MotherboardInfo`, `GpuDevice`, `ApiDisk` to the interface shapes; delete `MemoryInfoResponse`, `MemoryDockerInfo`, `MemorySystemInfo`, `MemoryZFSInfo`, `DiskThroughputResponse`, `DiskThroughputDevice`, `InterfaceStats`, `MotherboardTemperatures`; remove `RXSpeed` and `TXSpeed` from `NetworkInterface`; drop the `load` import if nothing else uses it.

`handlers/system/handlers.go`: delete the bindings and handlers for `system.get_memory_info`, `system.get_uptime`, `system.get_fs_info`, `system.get_sensor_info`, `system.get_disk_throughput`; drop the `host` import if `handleGetHostInfo` is its only user (it is not; `host.InfoWithContext` stays for host info).

`handlers/system/cpu.go`:

```go
package system

import (
	"context"

	"github.com/shirou/gopsutil/v4/cpu"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var cpuInfoCache hwSnapshotCache[*apischema.CPUInfoResponse]

// FetchCPUInfo returns CPU identity. It cannot change without a shutdown, so
// the first successful read serves the whole bridge session. Usage, load,
// frequencies and temperatures come from monitoring.get_live.
func FetchCPUInfo(ctx context.Context) (*apischema.CPUInfoResponse, error) {
	return cpuInfoCache.get(func() (*apischema.CPUInfoResponse, error) {
		info, err := cpu.InfoWithContext(ctx)
		if err != nil {
			return nil, err
		}
		if len(info) == 0 {
			return nil, errors.New("no cpu information available")
		}
		counts, _ := cpu.CountsWithContext(ctx, true)
		first := info[0]
		return &apischema.CPUInfoResponse{
			VendorID:  first.VendorID,
			ModelName: first.ModelName,
			Family:    first.Family,
			Model:     first.Model,
			MHz:       first.Mhz,
			Cores:     counts,
		}, nil
	})
}
```

(add `"errors"` to the imports). `cpu_test.go`:

```go
package system

import (
	"context"
	"testing"
)

func TestFetchCPUInfoCachesForProcessLifetime(t *testing.T) {
	cpuInfoCache = hwSnapshotCache[*apischema.CPUInfoResponse]{}
	first, err := FetchCPUInfo(context.Background())
	if err != nil {
		t.Skipf("no cpu info on this host: %v", err)
	}
	second, err := FetchCPUInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("second call must return the cached pointer")
	}
}
```

`handlers/system/motherboard.go`: remove the `fetchTemperatures` parameter and the temperature block; `FetchBaseboardInfo(ctx)` calls `fetchBaseboardInfo(ctx, "/sys/class/dmi/id")` and caches the result in a `hwSnapshotCache[apischema.MotherboardInfo]`. Update `motherboard_test.go` accordingly.

`handlers/system/gpu.go`: keep `FetchGPUInfo`, `gpuDeviceFromEntry`, `buildGPUEntry`, `populateDeviceInfo`, `normalizePCIAddress`, `setIfNonEmpty`, `readSysfsString`, `readSysfsBool`, `readlink`, `sanitizeUnknown`, `sanitizeZero`, `findGPUCardDir`, `pathIsDir`; replace `enrichGPUFromSysfs` with a static-only variant:

```go
func enrichGPUStaticFromSysfs(pciAddr string, entry map[string]any) {
	cardName, _, pciDir, ok := findGPUCardDir(pciAddr)
	if !ok {
		return
	}
	entry["drm_card"] = cardName
	setIfNonEmpty(entry, "driver_module", filepath.Base(readlink(filepath.Join(pciDir, "driver", "module"))))
	setIfNonEmpty(entry, "raw_class", readSysfsString(filepath.Join(pciDir, "class")))
	setIfNonEmpty(entry, "max_link_speed", sanitizeUnknown(readSysfsString(filepath.Join(pciDir, "max_link_speed"))))
	setIfNonEmpty(entry, "max_link_width", sanitizeZero(readSysfsString(filepath.Join(pciDir, "max_link_width"))))
	if v, ok := readSysfsBool(filepath.Join(pciDir, "boot_vga")); ok {
		entry["boot_vga"] = v
	}
}
```

Delete everything else that moved in Task 5 and the `nvidiaGPUStats` type. Wrap `FetchGPUInfo`'s result in `var gpuDevicesCache hwSnapshotCache[[]apischema.GpuDevice]`.

Delete `memory.go`, `fs.go`, `disk_throughput.go`, `disk_throughput_test.go`, `sensors.go`, `sensors_test.go`. Remove the moby client import from the system package if `memory.go` was its last user.

- [ ] **Step 5: Network and storage**

`handlers/network/handlers.go`: delete the `network.get_interface_stats` binding and handler. Delete `interface_stats.go` and `interface_stats_test.go`. In `network.go` remove the `RXSpeed`/`TXSpeed` assignments and the rate sampler they depend on; if `network_test.go` asserts rates, drop those assertions.

`handlers/storage/drives.go`: `buildDriveInfo` returns inventory only (delete the SMART and power blocks); delete `FetchSmartInfo`, `parseSmartInfoJSON`, `GetNVMePowerState`, `resolveCurrentNVMePowerState`, `nvmePsRe`, `nvmeStateRe`, `isNVMeDevice` if unused; keep `validDeviceNameRe`, `RunSmartTest`, `PollSmartTestStatus` and their types. In `smart_test_operation.go`, where the task reports completion (status `completed`), add:

```go
	if err := monitoring.RefreshSmart(ctx); err != nil {
		slog.Debug("smart refresh after self-test failed", "device", device, "err", err)
	}
```

importing `github.com/mordilloSan/LinuxIO/backend/bridge/handlers/monitoring`. The route stays a session task and keeps its progress stream.

- [ ] **Step 6: Generate, build, test**

```bash
make generate
make test-go GO_TEST_PKGS='./bridge/...'
```

Expected: PASS; the handler pattern test still passes because every `handlers.go` file holds only registration and adapters.

- [ ] **Step 7: Stage**

```bash
git add backend/bridge frontend/src/api/generated
git rm -q backend/bridge/handlers/system/memory.go backend/bridge/handlers/system/fs.go backend/bridge/handlers/system/disk_throughput.go backend/bridge/handlers/system/disk_throughput_test.go backend/bridge/handlers/system/sensors.go backend/bridge/handlers/system/sensors_test.go backend/bridge/handlers/network/interface_stats.go backend/bridge/handlers/network/interface_stats_test.go
```

---

### Task 8: Frontend reads live through `monitoring.get_live`

**Files:**
- Modify: `frontend/src/api/operation-query-invalidations.ts`, `frontend/src/routes/_authenticated/index.tsx`, `frontend/src/routes/_authenticated/-dashboard/{Processor,Memory,Network,Drive,FileSystem,SystemOverview,MotherBoard,Gpu}.tsx`, `FileSystem.test.tsx`, `MotherBoard.test.tsx`, `frontend/src/routes/_authenticated/hardware/route.tsx`, `hardware/-components/{HardwarePage,HardwareHistoryCards,hardwareQueryOptions}.tsx`, `frontend/src/components/cards/{SensorGroupCard,NetworkInterfaceCard}.tsx`, `frontend/src/routes/_authenticated/network/-components/NetworkInterfaceList.tsx`, `frontend/src/routes/_authenticated/storage/index.tsx`, `storage/-components/DiskOverview/index.tsx`, `frontend/src/utils/gpu.ts`, `frontend/src/components/cards/cardQueryOwnership.test.ts`, `frontend/src/routes/_authenticated/-query-ownership.test.ts`, `frontend/src/constants/liveCharts.ts`

**Interfaces:**
- Consumes generated `linuxio.monitoring.get_live` returning `MonitoringLive`, and the trimmed `CPUInfoResponse`, `GpuDevice`, `ApiDisk`, `MotherboardInfo`, `NetworkInterface`.
- Produces: `hardwareStableQueryOptions = { staleTime: CACHE_TTL_MS.ONE_DAY }`; `export const LIVE_QUERY = { ...linuxio.monitoring.get_live, refetchInterval: DASHBOARD_REFETCH_FAST_MS }` is **not** created; each card spreads `linuxio.monitoring.get_live` itself so the query-ownership tests keep seeing the route in the card file.
- `utils/gpu.ts` gains `export type GpuView = GpuDevice & Partial<MonitoringLiveGPU>` and `export const mergeGpus = (devices: GpuDevice[] | undefined, live: Record<string, MonitoringLiveGPU> | undefined): GpuView[]`.

- [ ] **Step 1: Invalidations and loaders**

`operation-query-invalidations.ts`: replace every `endpointQueryPrefix("system.get_fs_info")` and `endpointQueryPrefix("network.get_interface_stats")` with `endpointQueryPrefix("monitoring.get_live")`; change `"storage.run_smart_test": [endpointQueryPrefix("monitoring.get_live")]`.

`routes/_authenticated/index.tsx` loader:

```ts
    const queries: LoaderQueryOptions[] = [];
    const liveCards = ["overview", "cpu", "memory", "nic", "fs", "gpu", "drive"];
    if (liveCards.some((card) => !hiddenCards.has(card))) {
      queries.push(linuxio.monitoring.get_live);
    }
    if (!hiddenCards.has("overview")) {
      queries.push(linuxio.system.get_host_info, linuxio.system.get_server_time);
    }
    if (!hiddenCards.has("system"))
      queries.push(linuxio.system.get_health_summary);
    if (!hiddenCards.has("cpu")) queries.push(linuxio.system.get_cpu_info);
    if (!hiddenCards.has("nic")) queries.push(linuxio.network.get_network_info);
    if (!hiddenCards.has("mb"))
      queries.push(linuxio.system.get_motherboard_info);
    if (!hiddenCards.has("gpu")) queries.push(linuxio.system.get_gpu_info);
    if (!hiddenCards.has("drive")) queries.push(linuxio.storage.get_drive_info);
```

`hardware/route.tsx` loader: sensors prefetch becomes `{ ...linuxio.monitoring.get_live, ...hardwareSensorQueryOptions }`; the GPU prefetch uses `hardwareStableQueryOptions`. `storage/index.tsx` loader: replace `linuxio.system.get_fs_info` with `linuxio.monitoring.get_live`. `hardwareQueryOptions.ts`: `hardwareStableQueryOptions` becomes `{ staleTime: CACHE_TTL_MS.ONE_DAY }` and `hardwareGpuQueryOptions` is deleted (callers use the stable options).

- [ ] **Step 2: Processor card**

Replace the three queries in `Processor.tsx`:

```tsx
const selectAverageUsage = (live: MonitoringLive): number =>
  live.cpu.per_core_percent.length
    ? live.cpu.per_core_percent.reduce((sum, cpu) => sum + cpu, 0) /
      live.cpu.per_core_percent.length
    : 0;

// CpuTempBadge
  const selectBadge = useCallback(
    (live: MonitoringLive) => {
      const temperatures = live.cpu.temperatures ?? {};
      // ... unchanged body ...
    },
    [selectedSensor],
  );
  const { data: badge } = useSuspenseQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: selectBadge,
  });

// CpuStats
  const { data: cpuInfo } = useSuspenseQuery({
    ...linuxio.system.get_cpu_info,
    ...hardwareStableQueryOptions,
  });
  const { data: usage } = useSuspenseQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: selectUsageRows,
  });
```

with

```ts
const selectUsageRows = (live: MonitoringLive) => ({
  average: selectAverageUsage(live),
  peak: Math.max(...(live.cpu.per_core_percent.length ? live.cpu.per_core_percent : [0])),
  load: live.cpu.load_average,
});
const formatLoadAverage = (load?: number[]): string =>
  load && load.length === 3
    ? `${load[0].toFixed(2)} / ${load[1].toFixed(2)} / ${load[2].toFixed(2)}`
    : "N/A";
```

`CpuUsageGraph` selects `selectAverageUsage` from `linuxio.monitoring.get_live`. Import `hardwareStableQueryOptions` from the hardware folder or move it to `frontend/src/api/queryOptions.ts` if the import crosses route folders in a way the lint config forbids; either location is acceptable, choose the one that passes `make check-frontend-quiet`.

- [ ] **Step 3: Memory, FileSystem, SystemOverview, MotherBoard**

`Memory.tsx`: both queries become `linuxio.monitoring.get_live` at `DASHBOARD_REFETCH_FAST_MS`; `selectRamUsagePercent(live)` uses `live.memory.used_bytes / live.memory.total_bytes`; rows use `used_bytes`, `total_bytes`, `swap_total_bytes - swap_free_bytes`, `docker_used_bytes`, `zfs_arc_bytes`. Delete `DASHBOARD_REFETCH_MEMORY_MS` from `liveCharts.ts`.

`FileSystem.tsx`: `useSuspenseQuery({ ...linuxio.monitoring.get_live, refetchInterval: DASHBOARD_REFETCH_FAST_MS, select: (live) => live.filesystems })`; the rest is unchanged since `FilesystemInfo` keeps its shape.

`SystemOverview.tsx`: replace the uptime query with `{ ...linuxio.monitoring.get_live, refetchInterval: 30000, select: (live: MonitoringLive) => live.uptime_seconds }`.

`MotherBoard.tsx`: the badge selects from `linuxio.monitoring.get_live` at `DASHBOARD_REFETCH_SLOW_MS` with `live.motherboard_temperatures`; `MotherboardStats` reads `linuxio.system.get_motherboard_info` with `hardwareStableQueryOptions` and no refetch.

- [ ] **Step 4: Network, Drive, Gpu**

`Network.tsx`: identity from `linuxio.network.get_network_info` (fields `name`, `ipv4`, `mac`, `speed`; filter as today), rates from `linuxio.monitoring.get_live`:

```ts
const selectThroughput = useCallback(
  (live: MonitoringLive) => {
    const iface = live.interfaces[selected];
    return iface ? { rx: iface.rx_bytes_per_sec / 1024, tx: iface.tx_bytes_per_sec / 1024 } : null;
  },
  [selected],
);
```

`NetworkHeader` and `NetworkStats` keep a single `useSuspenseQuery` each on `get_network_info` (no `refetchInterval`; the interface list changes through invalidations). `NetworkGraphPane` keeps one polling query on the live route and passes `interfaceName={selected}`.

`Drive.tsx`: `DriveGraphPane` becomes

```ts
  const [{ data: driveName }, { data: rates }] = useSuspenseQueries({
    queries: [
      { ...linuxio.storage.get_drive_info, select: selectDriveName },
      {
        ...linuxio.monitoring.get_live,
        refetchInterval: 1000,
        select: (live: MonitoringLive) => live.disks,
      },
    ],
  });
  const device = rates[driveName];
  // readBytesPerSec={device?.read_bytes_per_sec ?? 0} writeBytesPerSec={device?.write_bytes_per_sec ?? 0}
```

`Gpu.tsx`:

```ts
  const [{ data: devices }, { data: liveGpus }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_gpu_info, ...hardwareStableQueryOptions },
      {
        ...linuxio.monitoring.get_live,
        refetchInterval: 2_000,
        select: (live: MonitoringLive) => live.gpus,
      },
    ],
  });
  const gpus = mergeGpus(devices, liveGpus);
```

`utils/gpu.ts`:

```ts
export type GpuView = GpuDevice & Partial<MonitoringLiveGPU>;

export const mergeGpus = (
  devices: GpuDevice[] | undefined,
  live: Record<string, MonitoringLiveGPU> | undefined,
): GpuView[] =>
  (devices ?? []).map((device) => ({ ...device, ...(live?.[device.address] ?? {}) }));
```

`getGpuType` takes `GpuView`. In `HardwareHistoryCards.tsx`, `GPUInfoCard` merges the same way (static query with stable options, live query at `hardwareSensorQueryOptions` cadence without `refetchInterval`, since the page's `SensorReadings` owns polling). `getGpuVramSummary` and `getGpuDriverSummary` take `GpuView`.

- [ ] **Step 5: Hardware sensors and storage**

`HardwarePage.tsx` `SensorReadings`: `useSuspenseQuery({ ...linuxio.monitoring.get_live, ...hardwareSensorQueryOptions, refetchInterval: 5_000, select: (live) => selectVisibleSensorGroupIdentities(live.sensors) })`. `SensorGroupCard.tsx` `SensorGroupCardLive`: `useQuery({ ...linuxio.monitoring.get_live, refetchOnMount: false, select: (live) => selectSensorGroup({ adapter, sourceIndex })(live.sensors) })`, keeping the memoized selector shape the ownership test expects.

`NetworkInterfaceList.tsx` and `NetworkInterfaceCard.tsx`: rates come from `linuxio.monitoring.get_live` selected per interface (`live.interfaces[iface.name]`); the list stays the single polling owner of `get_network_info`, and adds one polling observer on the live route; the card reads the live cache with `refetchOnMount: false`.

`DiskOverview/index.tsx`: replace the `system.get_fs_info` query with `{ ...linuxio.monitoring.get_live, refetchInterval: 10000, select: (live) => ({ filesystems: live.filesystems, smart: live.smart }) }`; `rawDrive.smart` becomes `liveSmart[drive.name]?.raw as SmartData | undefined`, `smartError` is `liveSmart[drive.name]?.error`, `power` is `liveSmart[drive.name]?.power`. Build `DriveInfo.smart`/`power` from the live map where the component maps `rawDrives` to `drives`.

- [ ] **Step 6: Tests**

- `cardQueryOwnership.test.ts`: `MemoryUsage` and `FsInfoCard` bodies now poll the live route; `Drive`'s `DriveGraphPane` keeps `useSuspenseQueries` with `selectDriveName`; `GpuInfo` gains `useSuspenseQueries: 1`; `NetworkHeader`/`NetworkStats` keep one `useSuspenseQuery` each; adjust the hook counts and `select` names to the code written above. The cache-only list keeps `SensorGroupCardLive` with `refetchOnMount: false`.
- `-query-ownership.test.ts`: the hardware assertion `expect(route).not.toContain("linuxio.monitoring.")` becomes `expect(route).not.toMatch(/linuxio\.monitoring\.get_[a-z]+_history/)`.
- `FileSystem.test.tsx`, `MotherBoard.test.tsx`: mock `monitoring.get_live` payloads instead of the removed routes.

- [ ] **Step 7: Run the frontend checks**

```bash
make check-frontend-quiet
```

Expected: PASS. Read `.cache/test-logs/` on failure and fix the card, not the test, unless the test encodes the old route.

- [ ] **Step 8: Stage**

```bash
git add frontend/src
```

---

### Task 9: Docs and full verification

**Files:**
- Modify: `docs/monitoring.md`, `docs/process-systemd-architecture.md` (privilege boundary row for `api.sock`), `docs/TODO/linuxio-monitoring.md` (status line), `docs/api-contract.md` if it lists the deleted routes

- [ ] **Step 1: Docs**

In `docs/monitoring.md` add the boundary rule, the route table from the spec's "Routes that move, shrink or go", the live payload sections including `filesystems`, `sensors`, `gpus`, `smart`, `motherboard_temperatures`, and the note that SMART `raw` is smartctl's `-x --json=c` output. Update the spec status line to "Plans 1 and 2 implemented; Plans 3 and 4 pending".

- [ ] **Step 2: Verification**

```bash
make check-backend-quiet
make check-frontend-quiet
make test-quiet
```

Expected: PASS.

- [ ] **Step 3: Runtime verification**

```bash
sudo make localinstall
curl -s --unix-socket /run/linuxio/monitoring/api.sock http://localhost/api/v1/live | jq '{cpu: .cpu.temperatures, fs: (.filesystems | length), sensors: (.sensors | length), gpus: (.gpus | keys), smart: (.smart | keys)}'
```

In the browser as root and as a non-root login: dashboard gauges move every second; the CPU temperature badge and motherboard badge show values when lm-sensors is installed; the drive graph follows the selected drive; the filesystem card lists mounts; the hardware page sensor cards update; the storage page renders SMART attributes, self-test logs and NVMe power states from the live payload; stopping `linuxio-monitoring.service` makes the gauges read zero without error toasts and the capability reason names the daemon; starting it again recovers within a second.

- [ ] **Step 4: Stage**

```bash
git add docs
```

Suggested commit message for the user:

```
feat(monitoring): move every sampled measurement behind monitoring.get_live

The bridge keeps hardware identity only, cached per session. Sensors,
GPU sysfs readers, filesystem usage, SMART and NVMe power state move
into the daemon and feed new live sections. Dashboard, hardware,
network and storage pages read one unprivileged live route.
```

---

## Self-Review Notes

- Spec coverage: boundary rule and route table (Task 7, 8), moved collectors (Tasks 3 to 6), live sections and allowlist mapping (Task 1, 6), static caching (Task 7), invalidations (Task 8), zeros on unreachable daemon (Task 7), tests per task, docs (Task 9). `system.get_processes` untouched per the spec's Plan 3 hand-off.
- Type consistency: `monitoringapi.LiveExtras` fields match what `buildLive` reads (Task 2, 6); `LiveGPU` JSON keys match the bridge's former entry keys so `liveGPUFromEntry` and the frontend spread both work (Task 1, 5, 8); `MonitoringLive` embeds `Live` so generated TypeScript exposes the same fields the cards select (Task 1, 7, 8).
- Deviation from the spec text: filesystem usage default cache is 0 (re-read every collection) rather than 15 minutes, matching today's behaviour; the operator opts into the cache.
