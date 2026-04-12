import React from "react";

import TimerCardsView from "./TimerCardsView";
import TimerTableView from "./TimerTableView";
import UnitListTab from "./UnitListTab";
import { UnitInfoPanel, formatUsec } from "./UnitViews";

import { linuxio } from "@/api";
import type { Timer, UnitInfo } from "@/api";
import { useViewMode } from "@/hooks/useViewMode";
import type { TableCardViewMode } from "@/types/config";

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
  return linuxio.dbus.list_timers.useQuery({
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
      viewMode={viewMode}
      setViewMode={setViewMode}
      data={data}
      isPending={isPending}
      isError={isError}
      error={error}
      searchPlaceholder="Search timers…"
      errorMessage="Failed to load timers"
      compareItems={compareTimersByName}
      matchesSearch={matchesTimerSearch}
      urlParam="timer"
      renderTableView={({ items, selected, onSelect, onDoubleClick }) => (
        <TimerTableView
          timers={items}
          selected={selected}
          onSelect={onSelect}
          onDoubleClick={onDoubleClick}
        />
      )}
      renderCardsView={({ items, expanded, onExpand, renderDetailPanel }) => (
        <TimerCardsView
          timers={items}
          expanded={expanded}
          onExpand={onExpand}
          renderDetailPanel={renderDetailPanel}
        />
      )}
      renderDetailPanel={(timer, onClose) => (
        <UnitInfoPanel
          unitName={timer.name}
          onClose={onClose}
          renderInfoRows={(info, isPending) =>
            buildTimerInfoRows(timer, info, isPending)
          }
        />
      )}
    />
  );
};

export default TimersTab;
