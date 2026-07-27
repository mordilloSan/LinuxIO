import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import type { TableCardViewMode, Timer, UnitInfo } from "@/api";
import { useViewMode } from "@/hooks/useViewMode";

import TimerCardsView from "./TimerCardsView";
import TimerTableView from "./TimerTableView";
import UnitListTab from "./UnitListTab";
import { formatUsec, UnitInfoPanel } from "./UnitViews";

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
  return useSuspenseQuery(
    linuxio.systemd.list_timers.queryOptions({
      refetchInterval: viewMode === "card" ? false : 5000,
    }),
  );
}

function buildTimerInfoRows(timer: Timer, info: UnitInfo | undefined) {
  return [
    {
      label: "Unit",
      value: String(info?.Unit ?? timer.unit ?? "—"),
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
  selected?: string;
}

const TimersTab = ({ onSelectedChange, selected }: TimersTabProps) => {
  const [viewMode, setViewMode] = useViewMode("timers.list", "table");
  const { data } = useTimersQuery(viewMode);

  return (
    <UnitListTab
      compareItems={compareTimersByName}
      data={data}
      matchesSearch={matchesTimerSearch}
      onSelectedChange={onSelectedChange}
      renderCardsView={({ items, expanded, onExpand, renderDetailPanel }) => (
        <TimerCardsView
          expanded={expanded}
          onExpand={onExpand}
          renderDetailPanel={renderDetailPanel}
          timers={items}
        />
      )}
      renderDetailPanel={(timer, onClose) => (
        <UnitInfoPanel
          onClose={onClose}
          renderInfoRows={(info) => buildTimerInfoRows(timer, info)}
          unitName={timer.name}
        />
      )}
      renderTableView={({ items, selected, onSelect, onDoubleClick }) => (
        <TimerTableView
          onDoubleClick={onDoubleClick}
          onSelect={onSelect}
          selected={selected}
          timers={items}
        />
      )}
      searchPlaceholder="Search timers…"
      selected={selected}
      setViewMode={setViewMode}
      viewMode={viewMode}
    />
  );
};

export default TimersTab;
