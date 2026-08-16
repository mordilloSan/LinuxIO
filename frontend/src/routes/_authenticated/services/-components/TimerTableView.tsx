import { useCallback } from "react";

import type { Timer } from "@/api";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import { useVirtualReorderableTableDnd } from "@/hooks/useReorderableTableDnd";

import { formatUsec } from "./unitFormatters";
import UnitStatusDot from "./UnitStatusDot";
import { MobileExpandedDetails, UnitTableView } from "./UnitViews";

interface TimerTableViewProps {
  onSelect?: (name: string | null) => void;
  timers: Timer[];
  surface: ReorderableSurface<Timer>;
}

const desktopColumns = [
  {
    field: "status",
    headerName: "Status",
    align: "left" as const,
    width: "120px",
    style: { paddingLeft: 8 },
  },
  { field: "name", headerName: "Name", align: "left" as const, width: "220px" },
  { field: "unit", headerName: "Unit", align: "left" as const, width: "220px" },
  {
    field: "next_elapse",
    headerName: "Next Elapse",
    align: "left" as const,
    width: "180px",
  },
  { field: "last_trigger", headerName: "Last Trigger", align: "left" as const },
];

const mobileColumns = [
  {
    field: "status",
    headerName: "Status",
    align: "left" as const,
    width: "110px",
    style: { paddingLeft: 8 },
  },
  { field: "name", headerName: "Name", align: "left" as const },
];

const getTimerRowKey = (timer: Timer) => timer.name;

const renderTimerMainRow = (timer: Timer, isMobile: boolean) => [
  <>
    <UnitStatusDot activeState={timer.active_state} />
    {timer.active_state}
  </>,
  timer.name,
  ...(isMobile
    ? []
    : [
        timer.unit || "—",
        formatUsec(timer.next_elapse_usec),
        formatUsec(timer.last_trigger_usec),
      ]),
];

const renderTimerMobileExpandedContent = (timer: Timer) => (
  <MobileExpandedDetails
    rows={[
      { label: "Unit", value: timer.unit || "—" },
      { label: "Next", value: formatUsec(timer.next_elapse_usec) },
      { label: "Last", value: formatUsec(timer.last_trigger_usec) },
    ]}
  />
);

const TimerTableView = ({ surface, timers, onSelect }: TimerTableViewProps) => {
  const handleSelect = useCallback(
    (key: string | number | null) =>
      onSelect?.(typeof key === "string" ? key : null),
    [onSelect],
  );

  const dnd = useVirtualReorderableTableDnd<Timer, Timer>({ surface });

  return (
    <UnitTableView
      dnd={dnd}
      data={timers}
      desktopColumns={desktopColumns}
      emptyMessage="No timers found."
      getRowKey={getTimerRowKey}
      mobileColumns={mobileColumns}
      onSelect={handleSelect}
      renderMainRow={renderTimerMainRow}
      renderMobileExpandedContent={renderTimerMobileExpandedContent}
    />
  );
};

export default TimerTableView;
