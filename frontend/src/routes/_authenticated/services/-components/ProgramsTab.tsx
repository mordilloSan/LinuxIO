import { useQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import { RoutedTabSearch } from "@/components/tabbar";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";

import ProcessVirtualTable from "./ProcessVirtualTable";

export const PROCESS_REFRESH_INTERVAL_MS = 2_000;

interface ProgramsTabProps {
  filter: string;
  onFilterChange: (filter: string) => void;
}

const ProgramsTab = ({ filter, onFilterChange }: ProgramsTabProps) => {
  const programs = useQuery({
    ...linuxio.monitoring.get_programs,
    refetchInterval: PROCESS_REFRESH_INTERVAL_MS,
  });
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
          aria-label="Filter programs"
          clearOnDocumentEscape
          onChange={onFilterChange}
          placeholder="Filter by program name…"
          value={filter}
        />
      </RoutedTabSearch>

      {programs.error && (
        <AppTypography color="error" variant="body2">
          Unable to load programs: {programs.error.message}
        </AppTypography>
      )}
      {programs.isLoading && (
        <AppTypography color="text.secondary" variant="body2">
          Loading programs…
        </AppTypography>
      )}

      <ProcessVirtualTable
        data={(programs.data?.items ?? []).filter((program) =>
          program.name.toLocaleLowerCase().includes(programFilter),
        )}
        mode="programs"
      />
    </div>
  );
};

export default ProgramsTab;
