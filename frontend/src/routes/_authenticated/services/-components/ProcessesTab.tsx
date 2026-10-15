import { useQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import { RoutedTabSearch } from "@/components/tabbar";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";

import { filterProcessRows, type ProcessView } from "./processData";
import ProcessVirtualTable from "./ProcessVirtualTable";

export const PROCESS_REFRESH_INTERVAL_MS = 2_000;

interface ProcessesTabProps {
  filter: string;
  onFilterChange: (filter: string) => void;
  view: ProcessView;
}

const ProcessesTab = ({ filter, onFilterChange, view }: ProcessesTabProps) => {
  const processes = useQuery({
    ...linuxio.monitoring.get_processes,
    enabled: view === "processes",
    refetchInterval: PROCESS_REFRESH_INTERVAL_MS,
  });
  const programs = useQuery({
    ...linuxio.monitoring.get_programs,
    enabled: view === "programs",
    refetchInterval: PROCESS_REFRESH_INTERVAL_MS,
  });
  const query = view === "processes" ? processes : programs;
  const programFilter = filter.trim().toLocaleLowerCase();

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 12,
        height: "100%",
        minHeight: 0,
        minWidth: 0,
      }}
    >
      <RoutedTabSearch active={filter !== ""}>
        <AppHeaderSearch
          aria-label="Filter processes"
          clearOnDocumentEscape
          onChange={onFilterChange}
          placeholder={
            view === "programs"
              ? "Filter by program name…"
              : "Filter by name, command line, or user…"
          }
          value={filter}
        />
      </RoutedTabSearch>

      {query.error && (
        <AppTypography color="error" variant="body2">
          Unable to load {view}: {query.error.message}
        </AppTypography>
      )}
      {query.isLoading && (
        <AppTypography color="text.secondary" variant="body2">
          Loading {view}…
        </AppTypography>
      )}

      {view === "processes" ? (
        <ProcessVirtualTable
          data={filterProcessRows(processes.data?.items ?? [], filter)}
          mode="processes"
        />
      ) : (
        <ProcessVirtualTable
          data={(programs.data?.items ?? []).filter((program) =>
            program.name.toLocaleLowerCase().includes(programFilter),
          )}
          mode="programs"
        />
      )}
    </div>
  );
};

export default ProcessesTab;
