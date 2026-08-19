import { useSuspenseQuery } from "@tanstack/react-query";

import type { linuxio } from "@/api";
import type { TableCardViewMode, Timer, UnitInfo } from "@/api";

import TimerCardsView from "./TimerCardsView";
import TimerTableView from "./TimerTableView";
import { formatUsec } from "./unitFormatters";
import UnitListTab from "./UnitListTab";
import { UnitInfoPanel } from "./UnitViews";

function compareTimersByName(a: Timer, b: Timer): number {
  return a.name.localeCompare(b.name, undefined, {
    numeric: true,
    sensitivity: "base",
  });
}

function matchesTimerSearch(timer: Timer, search: string): boolean {
  return (
    timer.name.toLowerCase().includes(search) ||
    (timer.description?.toLowerCase().includes(search) ?? false) ||
    timer.unit.toLowerCase().includes(search)
  );
}

function useTimersQuery(
  listQueryOptions: typeof linuxio.systemd.list_timers,
  viewMode: TableCardViewMode,
) {
  return useSuspenseQuery({
    ...listQueryOptions,
    refetchInterval: viewMode === "card" ? false : 5000,
  });
}

function buildTimerInfoRows(timer: Timer, info: UnitInfo | undefined) {
  return [
    {
      label: "Unit",
      value: info?.Unit ?? timer.unit ?? "—",
      hidden: !info && !timer.unit,
    },
    {
      label: "Next",
      value: formatUsec(info?.NextElapseUSec ?? timer.next_elapse_usec),
    },
    {
      label: "Last",
      value: formatUsec(info?.LastTriggerUSec ?? timer.last_trigger_usec),
    },
  ];
}

interface TimersTabProps {
  listQueryOptions: typeof linuxio.systemd.list_timers;
  onSelectedChange: (name: string | null) => void;
  selected?: string;
  selectedQueryOptions:
    | ReturnType<typeof linuxio.systemd.get_unit_info>
    | undefined;
  viewMode: TableCardViewMode;
}

const TimersTab = ({
  listQueryOptions,
  onSelectedChange,
  selected,
  selectedQueryOptions,
  viewMode,
}: TimersTabProps) => {
  const { data } = useTimersQuery(listQueryOptions, viewMode);

  return (
    <UnitListTab
      compareItems={compareTimersByName}
      data={data}
      matchesSearch={matchesTimerSearch}
      onSelectedChange={onSelectedChange}
      renderCardsView={({
        items,
        expanded,
        onExpand,
        renderDetailPanel,
        surface,
      }) => (
        <TimerCardsView
          expanded={expanded}
          onExpand={onExpand}
          renderDetailPanel={renderDetailPanel}
          timers={items}
          surface={surface}
        />
      )}
      renderDetailPanel={(timer, onClose) => (
        <UnitInfoPanel
          onClose={onClose}
          queryOptions={
            selected === timer.name ? selectedQueryOptions : undefined
          }
          renderInfoRows={(info) => buildTimerInfoRows(timer, info)}
          unitName={timer.name}
        />
      )}
      renderTableView={({ items, onSelect, surface }) => (
        <TimerTableView onSelect={onSelect} timers={items} surface={surface} />
      )}
      searchPlaceholder="Search timers…"
      selected={selected}
      surfaceId="timers.list"
      viewMode={viewMode}
    />
  );
};

export default TimersTab;
