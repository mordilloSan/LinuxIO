import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
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

function useTimersQuery(viewMode: TableCardViewMode) {
  return useSuspenseQuery({
    ...linuxio.systemd.list_timers,
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
  onSelectedChange: (name: string | null) => void;
  onViewModeChange: (next: TableCardViewMode) => void;
  selected?: string;
  viewMode: TableCardViewMode;
}

const TimersTab = ({
  onSelectedChange,
  onViewModeChange,
  selected,
  viewMode,
}: TimersTabProps) => {
  const { data } = useTimersQuery(viewMode);

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
          renderInfoRows={(info) => buildTimerInfoRows(timer, info)}
          unitName={timer.name}
        />
      )}
      renderTableView={({
        items,
        selected,
        onSelect,
        onDoubleClick,
        surface,
      }) => (
        <TimerTableView
          onDoubleClick={onDoubleClick}
          onSelect={onSelect}
          selected={selected}
          timers={items}
          surface={surface}
        />
      )}
      searchPlaceholder="Search timers…"
      selected={selected}
      setViewMode={onViewModeChange}
      surfaceId="timers.list"
      viewMode={viewMode}
    />
  );
};

export default TimersTab;
