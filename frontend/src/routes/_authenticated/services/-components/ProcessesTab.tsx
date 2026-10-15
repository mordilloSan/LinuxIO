import { useQuery } from "@tanstack/react-query";

import { linuxio, type Program } from "@/api";
import TabSelector from "@/components/tabbar/TabSelector";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";

import { filterProcessRows, type ProcessView } from "./processData";
import ProcessVirtualTable from "./ProcessVirtualTable";

const REFRESH_INTERVAL_MS = 2_000;

interface ProcessesPageProps {
  filter: string;
  onFilterChange: (filter: string) => void;
  onViewChange: (view: ProcessView) => void;
  view: ProcessView;
}

const ProcessesPage = ({
  filter,
  onFilterChange,
  onViewChange,
  view,
}: ProcessesPageProps) => {
  const processes = useQuery({
    ...linuxio.monitoring.get_processes,
    refetchInterval: REFRESH_INTERVAL_MS,
  });
  const programs = useQuery({
    ...linuxio.monitoring.get_programs,
    enabled: view === "programs",
    refetchInterval: REFRESH_INTERVAL_MS,
  });
  const processItems = filterProcessRows(processes.data?.items ?? [], filter);
  const programItems = (programs.data?.items ?? []).filter((program: Program) =>
    program.name
      .toLocaleLowerCase()
      .includes(filter.trim().toLocaleLowerCase()),
  );
  const count = processes.data?.count;
  const error =
    processes.error ?? (view === "programs" ? programs.error : null);
  const loading =
    processes.isLoading || (view === "programs" && programs.isLoading);

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 16,
        height: "100%",
        minHeight: 0,
        padding: 20,
      }}
    >
      <div
        style={{
          alignItems: "center",
          display: "flex",
          flexWrap: "wrap",
          gap: 12,
          justifyContent: "space-between",
        }}
      >
        <div>
          <AppTypography gutterBottom variant="h2">
            Processes
          </AppTypography>
          <AppTypography color="text.secondary" variant="body2">
            {count?.total ?? 0} total · {count?.running ?? 0} running ·{" "}
            {count?.sleeping ?? 0} sleeping
          </AppTypography>
        </div>
        <TabSelector
          onChange={onViewChange}
          options={[
            { value: "processes", label: "Processes" },
            { value: "programs", label: "Programs" },
          ]}
          value={view}
        />
      </div>

      <AppTextField
        aria-label="Filter processes"
        fullWidth
        onChange={(event) => onFilterChange(event.target.value)}
        placeholder={
          view === "programs"
            ? "Filter by program name"
            : "Filter by name, command line, or user"
        }
        size="small"
        value={filter}
      />

      {error && (
        <AppTypography color="error" variant="body2">
          Unable to load {view}:{" "}
          {error instanceof Error ? error.message : "unknown error"}
        </AppTypography>
      )}
      {loading && !error && (
        <AppTypography color="text.secondary" variant="body2">
          Loading {view}…
        </AppTypography>
      )}

      {view === "processes" ? (
        <ProcessVirtualTable data={processItems} mode="processes" />
      ) : (
        <ProcessVirtualTable data={programItems} mode="programs" />
      )}
    </div>
  );
};

export default ProcessesPage;
