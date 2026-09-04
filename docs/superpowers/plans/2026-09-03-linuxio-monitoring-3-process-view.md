# linuxio-monitoring Plan 3: Process View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A read-only `/processes` page backed by the daemon's live process and program samplers, replacing the bridge's `system.get_processes` stub.

**Architecture:** The daemon's process and program domain types move to `backend/monitoring/api` so the bridge can decode `/api/v1/processes` and `/api/v1/programs` from `api.sock`. Two new unprivileged bridge routes wrap them and return empty lists when the daemon is unreachable. A new authenticated route renders a virtualized table with a processes/programs toggle, sorting and a text filter, polling every two seconds.

**Tech Stack:** Go, React 19, TanStack Router and Query, `AppVirtualTable`, `TabSelector`, vitest.

**Spec:** `docs/TODO/linuxio-monitoring.md` ("Process view")

**Depends on:** Plans 1 and 2.

## Global Constraints

- Plan 1's Global Constraints apply.
- Both routes are unprivileged and `RetrySafe`; process listings are visible to every login the way `/proc` is.
- Read-only. No kill or signal action.
- The page polls `monitoring.get_processes` (and `monitoring.get_programs` when the toggle shows programs) every 2 seconds with one polling observer per active list.
- Frontend styling through `components/ui` and `--app-*` variables only; the table is `AppVirtualTable`, which is the only place `useMemo` is acceptable for column definitions.

---

## File Structure

| Path | Responsibility |
|---|---|
| `backend/monitoring/api/process.go` | `Process`, `ProcessMemory`, `ProcessIO`, `ProcessCount`, `Program` wire types. |
| `backend/monitoring/internal/domain/process/process.go` | Becomes aliases onto the api types. |
| `backend/bridge/apischema/models.go` | `MonitoringProcesses`, `MonitoringPrograms`. |
| `backend/bridge/handlers/monitoring/processes.go` (+`_test.go`) | `FetchProcesses`, `FetchPrograms`, handlers. |
| `backend/bridge/handlers/monitoring/handlers.go` | Route bindings. |
| `backend/bridge/handlers/system/{handlers.go,process.go}` | Stub route removed. |
| `frontend/src/icons/svg.tsx` | `ActivityIcon`. |
| `frontend/src/routes/_authenticated/processes/index.tsx` | Route, loader, navigation entry. |
| `frontend/src/routes/_authenticated/processes/-components/ProcessesPage.tsx` (+`.test.tsx`) | Page: toggle, filter, counts, table. |
| `frontend/src/routes/_authenticated/processes/-components/processColumns.tsx` | Column definitions and formatters. |
| `docs/monitoring.md`, `docs/TODO/linuxio-monitoring.md` | Docs and status. |

---

### Task 1: Process wire types shared by daemon and bridge

**Files:**
- Create: `backend/monitoring/api/process.go`
- Modify: `backend/monitoring/internal/domain/process/process.go`

**Interfaces:**
- Produces in `backend/monitoring/api`:

```go
type ProcessMemory struct {
	RSS    uint64 `json:"rss"`
	VMS    uint64 `json:"vms"`
	HWM    uint64 `json:"hwm,omitempty"`
	Data   uint64 `json:"data,omitempty"`
	Stack  uint64 `json:"stack,omitempty"`
	Locked uint64 `json:"locked,omitempty"`
	Swap   uint64 `json:"swap,omitempty"`
}

type ProcessIO struct {
	ReadCount               uint64 `json:"read_count,omitempty"`
	WriteCount              uint64 `json:"write_count,omitempty"`
	ReadBytes               uint64 `json:"read_bytes,omitempty"`
	WriteBytes              uint64 `json:"write_bytes,omitempty"`
	DiskReadBytes           uint64 `json:"disk_read_bytes,omitempty"`
	DiskWriteBytes          uint64 `json:"disk_write_bytes,omitempty"`
	DiskReadBytesPerSecond  uint64 `json:"disk_read_bytes_per_second,omitempty"`
	DiskWriteBytesPerSecond uint64 `json:"disk_write_bytes_per_second,omitempty"`
}

type Process struct {
	PID           int32         `json:"pid"`
	Name          string        `json:"name"`
	Cmdline       []string      `json:"cmdline,omitempty"`
	Username      string        `json:"username,omitempty"`
	Status        string        `json:"status,omitempty"`
	NumThreads    int32         `json:"num_threads,omitempty"`
	CPUPercent    float64       `json:"cpu_percent"`
	MemoryPercent float64       `json:"memory_percent"`
	MemoryInfo    ProcessMemory `json:"memory_info"`
	Nice          int32         `json:"nice,omitempty"`
	CreateTime    int64         `json:"create_time,omitempty"`
	IOCounters    ProcessIO     `json:"io_counters"`
	ContainerID   string        `json:"container_id,omitempty"`
	ContainerName string        `json:"container_name,omitempty"`
}

type ProcessCount struct {
	Total    int `json:"total"`
	Running  int `json:"running"`
	Sleeping int `json:"sleeping"`
	Stopped  int `json:"stopped,omitempty"`
	Zombie   int `json:"zombie,omitempty"`
	Blocked  int `json:"blocked,omitempty"`
	Idle     int `json:"idle,omitempty"`
	Thread   int `json:"thread"`
	PIDMax   int `json:"pid_max,omitempty"`
}

type Program struct {
	Name           string  `json:"name"`
	Count          int     `json:"count"`
	CPUPercent     float64 `json:"cpu_percent"`
	MemoryPercent  float64 `json:"memory_percent"`
	MemoryRSSBytes uint64  `json:"memory_rss_bytes"`
	PIDs           []int32 `json:"pids,omitempty"`
}
```

- `domain/process` keeps its package name and exports `type MemoryInfo = monitoringapi.ProcessMemory`, `type IOCounters = monitoringapi.ProcessIO`, `type Process = monitoringapi.Process`, `type Count = monitoringapi.ProcessCount`, `type Program = monitoringapi.Program`, so no daemon call site changes.

- [ ] **Step 1: Create `api/process.go`** with the block above (copy the JSON tags exactly from `domain/process/process.go` so the wire format does not change).

- [ ] **Step 2: Alias the domain package**

Replace the struct definitions in `domain/process/process.go` with the five aliases and the import `monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"`. Keep any methods or helper functions the file defines; if a method is defined on one of the aliased types, move it into `api/process.go` because methods cannot be declared on aliases of types from another package.

- [ ] **Step 3: Compile and run the daemon tests**

```bash
make test-go GO_TEST_PKGS='./monitoring/...'
```

Expected: PASS with no behaviour change.

- [ ] **Step 4: Stage**

```bash
git add backend/monitoring/api/process.go backend/monitoring/internal/domain/process
```

---

### Task 2: Bridge routes `monitoring.get_processes` and `monitoring.get_programs`

**Files:**
- Create: `backend/bridge/handlers/monitoring/processes.go`, `backend/bridge/handlers/monitoring/processes_test.go`
- Modify: `backend/bridge/apischema/models.go`, `backend/bridge/handlers/monitoring/handlers.go`, `backend/bridge/handlers/system/handlers.go`
- Delete: `backend/bridge/handlers/system/process.go`

**Interfaces:**
- Produces:

```go
// apischema
type MonitoringProcesses struct {
	CapturedAtMs int64                      `json:"captured_at_ms"`
	Count        monitoringapi.ProcessCount `json:"count"`
	Items        []monitoringapi.Process    `json:"items"`
}

type MonitoringPrograms struct {
	CapturedAtMs int64                   `json:"captured_at_ms"`
	Items        []monitoringapi.Program `json:"items"`
}

// monitoring package
func FetchProcesses(ctx context.Context) (apischema.MonitoringProcesses, error)
func FetchPrograms(ctx context.Context) (apischema.MonitoringPrograms, error)
```

Routes `monitoring.get_processes` and `monitoring.get_programs`, `apischema.NoRequest`, unprivileged, `RetrySafe`. On `ErrUnavailable` the handlers return the type with empty `Items` and zero timestamp, no error. `system.get_processes` and `apischema.ProcessInfo` are deleted.

- [ ] **Step 1: Write the failing tests**

`processes_test.go`:

```go
package monitoring

import (
	"context"
	"net/http"
	"syscall"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestFetchProcessesDecodesDaemonPayload(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/processes" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{
			"captured_at": 1700000000000,
			"count": {"total": 2, "running": 1, "sleeping": 1, "thread": 40},
			"items": [
				{"pid": 1, "name": "systemd", "username": "root", "status": "sleeping", "cpu_percent": 0.1, "memory_percent": 0.2, "memory_info": {"rss": 12345, "vms": 1}, "io_counters": {"disk_read_bytes_per_second": 10}},
				{"pid": 4242, "name": "nginx", "cmdline": ["nginx", "-g", "daemon off;"], "cpu_percent": 3.5, "memory_percent": 1, "memory_info": {"rss": 1}, "io_counters": {}, "container_id": "abc123def456", "container_name": "web"}
			]
		}`), nil
	})
	got, err := FetchProcesses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.CapturedAtMs != 1700000000000 || got.Count.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("got = %+v", got)
	}
	if got.Items[1].ContainerName != "web" || got.Items[1].Cmdline[2] != "daemon off;" || got.Items[0].IOCounters.DiskReadBytesPerSecond != 10 {
		t.Fatalf("items = %+v", got.Items)
	}
}

func TestFetchProgramsDecodesDaemonPayload(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/programs" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"captured_at": 5, "items": [{"name": "nginx", "count": 3, "cpu_percent": 4, "memory_percent": 2, "memory_rss_bytes": 999, "pids": [1, 2, 3]}]}`), nil
	})
	got, err := FetchPrograms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.CapturedAtMs != 5 || len(got.Items) != 1 || got.Items[0].Count != 3 || got.Items[0].PIDs[2] != 3 {
		t.Fatalf("got = %+v", got)
	}
}

func TestProcessHandlersReturnEmptyWhenUnavailable(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) { return nil, syscall.ECONNREFUSED })
	processes, err := handleGetProcesses(context.Background(), apischema.NoRequest{})
	if err != nil || processes.Items == nil || len(processes.Items) != 0 {
		t.Fatalf("processes = %+v err=%v", processes, err)
	}
	programs, err := handleGetPrograms(context.Background(), apischema.NoRequest{})
	if err != nil || programs.Items == nil || len(programs.Items) != 0 {
		t.Fatalf("programs = %+v err=%v", programs, err)
	}
}
```

- [ ] **Step 2: Run to see it fail**

```bash
make test-go GO_TEST_PKGS=./bridge/handlers/monitoring/... GO_TEST_FLAGS='-run "TestFetchProcesses|TestFetchPrograms|TestProcessHandlers"'
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement**

`apischema/models.go`: add the two types from the interface block; delete `ProcessInfo`.

`processes.go`:

```go
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const maxProcessPayloadBytes = 16 << 20

type daemonProcesses struct {
	CapturedAt int64                      `json:"captured_at"`
	Count      monitoringapi.ProcessCount `json:"count"`
	Items      []monitoringapi.Process    `json:"items"`
}

type daemonPrograms struct {
	CapturedAt int64                   `json:"captured_at"`
	Items      []monitoringapi.Program `json:"items"`
}

func fetchAPIJSON(ctx context.Context, route string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+route, nil)
	if err != nil {
		return fmt.Errorf("create %s request: %w", route, err)
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s returned %s", ErrUnavailable, route, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProcessPayloadBytes)).Decode(dst); err != nil {
		return fmt.Errorf("decode %s: %w", route, err)
	}
	return nil
}

// FetchProcesses reads the daemon's live process list. Any session may call
// it; the daemon samples on request with the one-second reuse.
func FetchProcesses(ctx context.Context) (apischema.MonitoringProcesses, error) {
	var payload daemonProcesses
	if err := fetchAPIJSON(ctx, "/api/v1/processes", &payload); err != nil {
		return apischema.MonitoringProcesses{}, err
	}
	items := payload.Items
	if items == nil {
		items = []monitoringapi.Process{}
	}
	return apischema.MonitoringProcesses{CapturedAtMs: payload.CapturedAt, Count: payload.Count, Items: items}, nil
}

func FetchPrograms(ctx context.Context) (apischema.MonitoringPrograms, error) {
	var payload daemonPrograms
	if err := fetchAPIJSON(ctx, "/api/v1/programs", &payload); err != nil {
		return apischema.MonitoringPrograms{}, err
	}
	items := payload.Items
	if items == nil {
		items = []monitoringapi.Program{}
	}
	return apischema.MonitoringPrograms{CapturedAtMs: payload.CapturedAt, Items: items}, nil
}
```

Refactor Plan 1's `FetchLive` to use `fetchAPIJSON` as well, keeping its own 4 MiB limit if the linter accepts a parameter, otherwise leave both as they are.

`handlers.go`: add the bindings and handlers:

```go
	apischema.Call[apischema.NoRequest, apischema.MonitoringProcesses]("monitoring.get_processes", apischema.RetrySafe()).Handle(handleGetProcesses),
	apischema.Call[apischema.NoRequest, apischema.MonitoringPrograms]("monitoring.get_programs", apischema.RetrySafe()).Handle(handleGetPrograms),
```

```go
func handleGetProcesses(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringProcesses, error) {
	processes, err := FetchProcesses(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return apischema.MonitoringProcesses{}, ctx.Err()
		}
		return apischema.MonitoringProcesses{Items: []monitoringapi.Process{}}, nil
	}
	return processes, nil
}

func handleGetPrograms(ctx context.Context, _ apischema.NoRequest) (apischema.MonitoringPrograms, error) {
	programs, err := FetchPrograms(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return apischema.MonitoringPrograms{}, ctx.Err()
		}
		return apischema.MonitoringPrograms{Items: []monitoringapi.Program{}}, nil
	}
	return programs, nil
}
```

`handlers/system/handlers.go`: delete the `system.get_processes` binding and `handleGetProcesses`; delete `process.go`.

- [ ] **Step 4: Generate and test**

```bash
make generate
make test-go GO_TEST_PKGS='./bridge/...'
```

Expected: PASS; generated types include `MonitoringProcesses`, `MonitoringPrograms`, `Process`, `Program`, `ProcessCount`; `ProcessInfo` is gone.

- [ ] **Step 5: Stage**

```bash
git add backend/bridge frontend/src/api/generated
git rm -q backend/bridge/handlers/system/process.go
```

---

### Task 3: The `/processes` page

**Files:**
- Create: `frontend/src/routes/_authenticated/processes/index.tsx`, `frontend/src/routes/_authenticated/processes/-components/ProcessesPage.tsx`, `frontend/src/routes/_authenticated/processes/-components/ProcessesPage.test.tsx`, `frontend/src/routes/_authenticated/processes/-components/processColumns.tsx`
- Modify: `frontend/src/icons/svg.tsx`, `frontend/src/routeTree.gen.ts` (regenerated by the dev tooling, never by hand)

**Interfaces:**
- Consumes `linuxio.monitoring.get_processes` and `linuxio.monitoring.get_programs`.
- Produces a route `/_authenticated/processes` with `staticData.navigation = { icon: ActivityIcon, position: 95, title: "Processes" }`, and search param `view` of `"processes" | "programs"` validated through `@/routes/-search`.

- [ ] **Step 1: Icon**

Append to `icons/svg.tsx`, matching the file's existing pattern:

```tsx
export const ActivityIcon = () => (
  <svg fill="none" height="20" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" viewBox="0 0 24 24" width="20">
    <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
  </svg>
);
```

- [ ] **Step 2: Route**

`processes/index.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { ActivityIcon } from "@/icons/svg";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import ProcessesPage from "./-components/ProcessesPage";

export const Route = createFileRoute("/_authenticated/processes/")({
  validateSearch: (search) => ({
    ...optionalString(search, "view"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.monitoring.get_processes]),
  component: ProcessesRoute,
  staticData: {
    navigation: {
      icon: ActivityIcon,
      position: 95,
      title: "Processes",
    },
  },
});

function ProcessesRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const view = search.view === "programs" ? "programs" : "processes";
  return (
    <ProcessesPage
      onViewChange={(next) =>
        void navigate({
          to: "/processes",
          search: (previous) => ({ ...previous, view: next === "processes" ? undefined : next }),
        })
      }
      view={view}
    />
  );
}
```

- [ ] **Step 3: Columns**

`processColumns.tsx`:

```tsx
import type { Process, Program } from "@/api";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize, formatThroughput } from "@/utils/formaters";

const percent = (value: number) => `${value.toFixed(1)}%`;

export const processColumns: AppVirtualTableColumnDef<Process>[] = [
  { accessorKey: "pid", header: "PID", size: 80 },
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => (
      <div style={{ minWidth: 0 }}>
        <AppTypography fontWeight={600} noWrap variant="body2">
          {row.original.name}
        </AppTypography>
        {row.original.cmdline?.length ? (
          <AppTypography color="text.secondary" noWrap variant="caption">
            {row.original.cmdline.join(" ")}
          </AppTypography>
        ) : null}
      </div>
    ),
  },
  { accessorKey: "username", header: "User", size: 120 },
  {
    accessorKey: "cpu_percent",
    header: "CPU",
    size: 90,
    cell: ({ row }) => percent(row.original.cpu_percent),
  },
  {
    id: "memory",
    accessorFn: (process) => process.memory_info.rss,
    header: "Memory",
    size: 110,
    cell: ({ row }) => formatFileSize(row.original.memory_info.rss),
  },
  {
    id: "disk_read",
    accessorFn: (process) => process.io_counters.disk_read_bytes_per_second ?? 0,
    header: "Read/s",
    size: 100,
    cell: ({ row }) => formatThroughput(row.original.io_counters.disk_read_bytes_per_second ?? 0),
  },
  {
    id: "disk_write",
    accessorFn: (process) => process.io_counters.disk_write_bytes_per_second ?? 0,
    header: "Write/s",
    size: 100,
    cell: ({ row }) => formatThroughput(row.original.io_counters.disk_write_bytes_per_second ?? 0),
  },
  { accessorKey: "num_threads", header: "Threads", size: 90 },
  {
    accessorKey: "container_name",
    header: "Container",
    size: 140,
    cell: ({ row }) => row.original.container_name ?? "",
  },
];

export const programColumns: AppVirtualTableColumnDef<Program>[] = [
  {
    accessorKey: "name",
    header: "Program",
    cell: ({ row }) => (
      <AppTypography fontWeight={600} noWrap variant="body2">
        {row.original.name}
      </AppTypography>
    ),
  },
  { accessorKey: "count", header: "Processes", size: 110 },
  {
    accessorKey: "cpu_percent",
    header: "CPU",
    size: 90,
    cell: ({ row }) => percent(row.original.cpu_percent),
  },
  {
    accessorKey: "memory_rss_bytes",
    header: "Memory",
    size: 110,
    cell: ({ row }) => formatFileSize(row.original.memory_rss_bytes),
  },
];

export const matchesProcessFilter = (process: Process, needle: string): boolean => {
  if (!needle) return true;
  const haystack = `${process.name} ${process.cmdline?.join(" ") ?? ""} ${process.username ?? ""} ${process.container_name ?? ""}`.toLowerCase();
  return haystack.includes(needle);
};

export const matchesProgramFilter = (program: Program, needle: string): boolean =>
  !needle || program.name.toLowerCase().includes(needle);
```

If `formatThroughput` or `formatFileSize` do not exist under those names in `@/utils/formaters`, use the file's existing byte and rate formatters; both are already used by `DriveGraph.tsx` and `Memory.tsx`.

- [ ] **Step 4: Page**

`ProcessesPage.tsx`:

```tsx
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import { TabSelector } from "@/components/tabbar/TabSelector";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";

import {
  matchesProcessFilter,
  matchesProgramFilter,
  processColumns,
  programColumns,
} from "./processColumns";

const PROCESS_REFETCH_MS = 2000;

export type ProcessesView = "processes" | "programs";

interface ProcessesPageProps {
  onViewChange: (view: ProcessesView) => void;
  view: ProcessesView;
}

const ProcessesPage = ({ onViewChange, view }: ProcessesPageProps) => {
  const [filter, setFilter] = useState("");
  const needle = filter.trim().toLowerCase();

  const processes = useQuery({
    ...linuxio.monitoring.get_processes,
    refetchInterval: PROCESS_REFETCH_MS,
  });
  const programs = useQuery({
    ...linuxio.monitoring.get_programs,
    enabled: view === "programs",
    refetchInterval: view === "programs" ? PROCESS_REFETCH_MS : false,
  });

  const count = processes.data?.count;
  const processRows = (processes.data?.items ?? []).filter((process) =>
    matchesProcessFilter(process, needle),
  );
  const programRows = (programs.data?.items ?? []).filter((program) =>
    matchesProgramFilter(program, needle),
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--app-space-4)", height: "100%", minHeight: 0 }}>
      <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: "var(--app-space-4)" }}>
        <TabSelector
          onChange={(next) => onViewChange(next === "programs" ? "programs" : "processes")}
          options={[
            { value: "processes", label: "Processes" },
            { value: "programs", label: "Programs" },
          ]}
          value={view}
        />
        <AppTextField
          aria-label="Filter processes"
          onChange={(event) => setFilter(event.target.value)}
          placeholder="Filter by name, command, user or container"
          size="small"
          style={{ minWidth: 280 }}
          value={filter}
        />
        {count ? (
          <AppTypography color="text.secondary" variant="body2">
            {count.total} processes · {count.running} running · {count.sleeping} sleeping · {count.thread} threads
          </AppTypography>
        ) : null}
      </div>
      {view === "processes" ? (
        <AppVirtualTable
          ariaLabel="Processes"
          columns={processColumns}
          data={processes.data ? processRows : undefined}
          density="compact"
          emptyMessage={processes.data ? "No processes match the filter." : "Waiting for linuxio-monitoring."}
          enableSorting
          getRowId={(process) => String(process.pid)}
        />
      ) : (
        <AppVirtualTable
          ariaLabel="Programs"
          columns={programColumns}
          data={programs.data ? programRows : undefined}
          density="compact"
          emptyMessage={programs.data ? "No programs match the filter." : "Waiting for linuxio-monitoring."}
          enableSorting
          getRowId={(program) => program.name}
        />
      )}
    </div>
  );
};

export default ProcessesPage;
```

Check `TabSelector`'s export shape (default or named) and `AppVirtualTable`'s required props (`getRowId`, `columns`, `data`) against `AppVirtualTableProps`; add any required prop the interface demands with the value the LVM table uses.

- [ ] **Step 5: Page test**

`ProcessesPage.test.tsx`, following the render helpers in `@/test/render` that other route component tests use:

```tsx
import { screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { renderWithProviders } from "@/test/render";

import ProcessesPage from "./ProcessesPage";

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      monitoring: {
        ...actual.linuxio.monitoring,
        get_processes: {
          queryKey: ["monitoring.get_processes"],
          queryFn: async () => ({
            captured_at_ms: 1,
            count: { total: 2, running: 1, sleeping: 1, thread: 9 },
            items: [
              { pid: 1, name: "systemd", username: "root", cpu_percent: 0.1, memory_percent: 0, memory_info: { rss: 1024, vms: 0 }, io_counters: {} },
              { pid: 7, name: "nginx", cmdline: ["nginx", "-g", "daemon off;"], cpu_percent: 2, memory_percent: 0, memory_info: { rss: 2048, vms: 0 }, io_counters: {}, container_name: "web" },
            ],
          }),
        },
        get_programs: {
          queryKey: ["monitoring.get_programs"],
          queryFn: async () => ({ captured_at_ms: 1, items: [] }),
        },
      },
    },
  };
});

describe("ProcessesPage", () => {
  it("lists processes and filters by text", async () => {
    renderWithProviders(<ProcessesPage onViewChange={() => {}} view="processes" />);
    expect(await screen.findByText("nginx")).toBeInTheDocument();
    expect(screen.getByText("systemd")).toBeInTheDocument();
    expect(screen.getByText(/2 processes/)).toBeInTheDocument();
  });
});
```

Adapt the mock shape to how `@/test/render` and other page tests stub `linuxio.*` query options (look at `Docker.test.tsx` in the dashboard folder for the established pattern and follow it instead of the sketch above if it differs).

- [ ] **Step 6: Frontend checks**

```bash
make check-frontend-quiet
```

Expected: PASS. The route tree regenerates during the check. If `-query-ownership.test.ts`'s "defaults query loaders to presence" rule flags the new loader, it already uses `loadRouteQueries` with the default freshness, which is what the rule wants.

- [ ] **Step 7: Stage**

```bash
git add frontend/src/icons/svg.tsx frontend/src/routes/_authenticated/processes frontend/src/routeTree.gen.ts
```

---

### Task 4: Docs and verification

- [ ] **Step 1: Docs**

`docs/monitoring.md`: add the two process routes and the page. `docs/TODO/linuxio-monitoring.md`: status "Plans 1 to 3 implemented; Plan 4 pending".

- [ ] **Step 2: Verification**

```bash
make check-backend-quiet
make check-frontend-quiet
make test-quiet
```

Expected: PASS.

- [ ] **Step 3: Runtime**

`sudo make localinstall`, open `/processes` as root and as a non-root login: rows update every two seconds, sorting by CPU puts the busiest process first, the filter narrows by command line, the programs toggle groups by name, and stopping the daemon shows the waiting message without an error toast.

- [ ] **Step 4: Stage**

```bash
git add docs
```

Suggested commit message for the user:

```
feat(monitoring): add read-only process view backed by linuxio-monitoring
```

---

## Self-Review Notes

- Spec coverage: routes, payload fields, page, toggle, sort, filter, 2-second polling, counts in the header, read-only (Task 2, 3); stub route deleted (Task 2).
- Type consistency: `monitoringapi.Process/Program/ProcessCount` (Task 1) are what `apischema.MonitoringProcesses/Programs` embed (Task 2) and what the generated `Process`/`Program` types give the columns (Task 3).
