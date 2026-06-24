import React from "react";

import TimerCardsView from "./TimerCardsView";
import TimerTableView from "./TimerTableView";
import UnitListTab from "./UnitListTab";
import { formatUsec, UnitInfoPanel } from "./UnitViews";

import { linuxio } from "@/api";
import type { TableCardViewMode, Timer, UnitInfo } from "@/api";
import { useViewMode } from "@/hooks/useViewMode";

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
  return linuxio.systemd.list_timers.useQuery({
    refetchInterval: viewMode === "card" ? false : 5000,
  });
}

function buildTimerInfoRows(
  timer: Timer,
  info: UnitInfo | undefined,
  isPending: boolean,
) {
  return [
    {
      label: "Unit",
      value: String(info?.Unit ?? timer.unit ?? "—"),
      hidden: isPending && !info && !timer.unit,
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

const TimersTab: React.FC = () => {
  const [viewMode, setViewMode] = useViewMode("timers.list", "table");
  const { data, isPending, isError, error } = useTimersQuery(viewMode);

  return (
    <UnitListTab
      compareItems={compareTimersByName}
      data={data}
      error={error}
      errorMessage="Failed to load timers"
      isError={isError}
      isPending={isPending}
      matchesSearch={matchesTimerSearch}
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
          renderInfoRows={(info, isPending) =>
            buildTimerInfoRows(timer, info, isPending)
          }
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
      setViewMode={setViewMode}
      urlParam="timer"
      viewMode={viewMode}
    />
  );
};

export default TimersTab;
