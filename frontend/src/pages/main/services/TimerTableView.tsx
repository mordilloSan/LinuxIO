import { TableCell } from "@mui/material";
import React from "react";

import { UnitTableView, formatUsec, statusDot } from "./UnitViews";

import type { Timer } from "@/api";

interface TimerTableViewProps {
  timers: Timer[];
  selected?: string | null;
  onSelect?: (name: string | null) => void;
  onDoubleClick?: (name: string) => void;
}

const desktopColumns = [
  {
    field: "status",
    headerName: "Status",
    align: "left" as const,
    width: "120px",
    sx: { paddingLeft: "8px" },
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
    sx: { paddingLeft: "8px" },
  },
  { field: "name", headerName: "Name", align: "left" as const },
];

const TimerTableView: React.FC<TimerTableViewProps> = ({
  timers,
  selected,
  onSelect,
  onDoubleClick,
}) => (
  <UnitTableView
    data={timers}
    desktopColumns={desktopColumns}
    mobileColumns={mobileColumns}
    getRowKey={(timer) => timer.name}
    selected={selected}
    onSelect={(key) => onSelect?.(typeof key === "string" ? key : null)}
    onDoubleClick={(key) => {
      if (typeof key === "string") {
        onDoubleClick?.(key);
      }
    }}
    renderMobileExpandedContent={(timer) => (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: 6,
          padding: "2px 0",
        }}
      >
        {[
          { label: "Unit", value: timer.unit || "—" },
          { label: "Next", value: formatUsec(timer.next_elapse_usec) },
          { label: "Last", value: formatUsec(timer.last_trigger_usec) },
        ].map(({ label, value }) => (
          <div key={label} style={{ display: "flex", gap: 12 }}>
            <span
              style={{
                fontSize: "0.6rem",
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                color: "var(--mui-palette-text-secondary)",
                width: 80,
                flexShrink: 0,
                paddingTop: 2,
              }}
            >
              {label}
            </span>
            <span style={{ fontSize: "0.8rem", fontWeight: 500 }}>{value}</span>
          </div>
        ))}
      </div>
    )}
    renderMainRow={(timer, isMobile) => (
      <>
        <TableCell sx={{ paddingLeft: "8px" }}>
          {statusDot(timer.active_state)}
          {timer.active_state}
        </TableCell>
        <TableCell>{timer.name}</TableCell>
        {!isMobile && (
          <>
            <TableCell>{timer.unit || "—"}</TableCell>
            <TableCell>{formatUsec(timer.next_elapse_usec)}</TableCell>
            <TableCell>{formatUsec(timer.last_trigger_usec)}</TableCell>
          </>
        )}
      </>
    )}
    emptyMessage="No timers found."
  />
);

export default TimerTableView;
