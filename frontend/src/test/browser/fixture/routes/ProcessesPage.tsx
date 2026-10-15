import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";

import {
  linuxio,
  type MonitoringProcessesResponse,
  type MonitoringProgramsResponse,
} from "@/api";
import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import ProcessesTab from "@/routes/_authenticated/services/-components/ProcessesTab";

const processesResponse: MonitoringProcessesResponse = {
  captured_at_ms: 0,
  count: {
    total: 2,
    running: 2,
    sleeping: 0,
    thread: 2,
  },
  items: [
    {
      pid: 101,
      name: "nginx",
      cmdline: ["nginx", "-g", "daemon off;"],
      username: "www-data",
      cpu_percent: 8,
      memory_percent: 2,
      memory_info: { rss: 12_000, vms: 20_000 },
      io_counters: {
        disk_read_bytes_per_second: 3_000,
        disk_write_bytes_per_second: 1_000,
      },
    },
    {
      pid: 102,
      name: "worker",
      cmdline: ["worker", "--queue", "jobs"],
      username: "root",
      cpu_percent: 2,
      memory_percent: 1,
      memory_info: { rss: 8_000, vms: 16_000 },
      io_counters: {
        disk_read_bytes_per_second: 500,
        disk_write_bytes_per_second: 700,
      },
    },
  ],
};

const programsResponse: MonitoringProgramsResponse = {
  captured_at_ms: 0,
  items: [
    {
      name: "nginx",
      count: 1,
      cpu_percent: 8,
      memory_percent: 2,
      memory_rss_bytes: 12_000,
      pids: [101],
    },
    {
      name: "worker",
      count: 1,
      cpu_percent: 2,
      memory_percent: 1,
      memory_rss_bytes: 8_000,
      pids: [102],
    },
  ],
};

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Infinity },
  },
});

queryClient.setQueryData(
  linuxio.monitoring.get_processes.queryKey,
  processesResponse,
);
queryClient.setQueryData(
  linuxio.monitoring.get_programs.queryKey,
  programsResponse,
);
linuxio.monitoring.get_processes.queryFn = async () => processesResponse;
linuxio.monitoring.get_programs.queryFn = async () => programsResponse;

const tabs = [
  { label: "Processes", to: "/services/processes" },
  { label: "Programs", to: "/services/programs" },
] as const satisfies readonly RoutedTab[];

export function ProcessesFixtureTab() {
  const { pathname, search } = useLocation();
  const navigate = useNavigate();
  const filter = typeof search.filter === "string" ? search.filter : "";
  const view = pathname.endsWith("/programs") ? "programs" : "processes";

  return (
    <ProcessesTab
      filter={filter}
      onFilterChange={(nextFilter) =>
        void navigate({
          replace: true,
          search: (previous) => ({
            ...previous,
            filter: nextFilter || undefined,
          }),
          to: `/services/${view}`,
        })
      }
      view={view}
    />
  );
}

export default function ProcessesFixtureLayout() {
  return (
    <QueryClientProvider client={queryClient}>
      <div style={{ height: "100dvh", minHeight: 0, overflow: "hidden" }}>
        <RoutedTabLayout tabs={tabs}>
          <Outlet />
        </RoutedTabLayout>
      </div>
    </QueryClientProvider>
  );
}
