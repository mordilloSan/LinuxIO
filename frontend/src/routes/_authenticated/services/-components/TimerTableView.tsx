import type { Timer } from "@/api";
import { AppTableCell } from "@/components/ui/AppTable";

import {
  formatUsec,
  MobileExpandedDetails,
  statusDot,
  UnitTableView,
} from "./UnitViews";

interface TimerTableViewProps {
  onDoubleClick?: (name: string) => void;
  onSelect?: (name: string | null) => void;
  selected?: string | null;
  timers: Timer[];
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

const TimerTableView = ({
  timers,
  selected,
  onSelect,
  onDoubleClick,
}: TimerTableViewProps) => (
  <UnitTableView
    data={timers}
    desktopColumns={desktopColumns}
    emptyMessage="No timers found."
    getRowKey={(timer) => timer.name}
    mobileColumns={mobileColumns}
    onDoubleClick={(key) => {
      if (typeof key === "string") {
        onDoubleClick?.(key);
      }
    }}
    onSelect={(key) => onSelect?.(typeof key === "string" ? key : null)}
    renderMainRow={(timer, isMobile) => (
      <>
        <AppTableCell style={{ paddingLeft: 8 }}>
          {statusDot(timer.active_state)}
          {timer.active_state}
        </AppTableCell>
        <AppTableCell>{timer.name}</AppTableCell>
        {!isMobile && (
          <>
            <AppTableCell>{timer.unit || "—"}</AppTableCell>
            <AppTableCell>{formatUsec(timer.next_elapse_usec)}</AppTableCell>
            <AppTableCell>{formatUsec(timer.last_trigger_usec)}</AppTableCell>
          </>
        )}
      </>
    )}
    renderMobileExpandedContent={(timer) => (
      <MobileExpandedDetails
        rows={[
          { label: "Unit", value: timer.unit || "—" },
          { label: "Next", value: formatUsec(timer.next_elapse_usec) },
          { label: "Last", value: formatUsec(timer.last_trigger_usec) },
        ]}
      />
    )}
    selected={selected}
  />
);

export default TimerTableView;
