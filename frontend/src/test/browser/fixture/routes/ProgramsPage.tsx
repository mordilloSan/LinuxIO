import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";

import { linuxio, type MonitoringProgramsResponse } from "@/api";
import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import ProgramsTab from "@/routes/_authenticated/services/-components/ProgramsTab";

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
  linuxio.monitoring.get_programs.queryKey,
  programsResponse,
);
linuxio.monitoring.get_programs.queryFn = async () => programsResponse;

const tabs = [
  { label: "Programs", to: "/services/programs" },
] as const satisfies readonly RoutedTab[];

export function ProgramsFixtureTab() {
  const { search } = useLocation();
  const navigate = useNavigate();
  const filter = typeof search.filter === "string" ? search.filter : "";

  return (
    <ProgramsTab
      filter={filter}
      onFilterChange={(nextFilter) =>
        void navigate({
          replace: true,
          search: (previous) => ({
            ...previous,
            filter: nextFilter || undefined,
          }),
          to: "/services/programs",
        })
      }
    />
  );
}

export default function ProgramsFixtureLayout() {
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
