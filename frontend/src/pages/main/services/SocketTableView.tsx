import { useTheme } from "@mui/material/styles";
import React from "react";

import { UnitTableView, statusDot } from "./UnitViews";

import type { Socket } from "@/api";
import Chip from "@/components/ui/AppChip";
import { AppTableCell } from "@/components/ui/AppTable";

interface SocketTableViewProps {
  sockets: Socket[];
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
    style: { paddingLeft: 8 },
  },
  { field: "name", headerName: "Name", align: "left" as const, width: "220px" },
  { field: "listen", headerName: "Listen", align: "left" as const },
  {
    field: "connections",
    headerName: "Connections",
    align: "right" as const,
    width: "130px",
  },
  {
    field: "accepted",
    headerName: "Accepted",
    align: "right" as const,
    width: "120px",
  },
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

const SocketTableView: React.FC<SocketTableViewProps> = ({
  sockets,
  selected,
  onSelect,
  onDoubleClick,
}) => {
  const theme = useTheme();

  return (
    <UnitTableView
      data={sockets}
      desktopColumns={desktopColumns}
      mobileColumns={mobileColumns}
      getRowKey={(socket) => socket.name}
      selected={selected}
      onSelect={(key) => onSelect?.(typeof key === "string" ? key : null)}
      onDoubleClick={(key) => {
        if (typeof key === "string") {
          onDoubleClick?.(key);
        }
      }}
      renderMobileExpandedContent={(socket) => (
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 6,
            padding: "2px 0",
          }}
        >
          {[
            { label: "Listen", value: socket.listen.join(", ") || "—" },
            { label: "Connections", value: String(socket.n_connections) },
            { label: "Accepted", value: String(socket.n_accepted) },
          ].map(({ label, value }) => (
            <div key={label} style={{ display: "flex", gap: 12 }}>
              <span
                style={{
                  fontSize: "0.6rem",
                  textTransform: "uppercase",
                  letterSpacing: "0.06em",
                  color: "var(--app-palette-text-secondary)",
                  width: 80,
                  flexShrink: 0,
                  paddingTop: 2,
                }}
              >
                {label}
              </span>
              <span style={{ fontSize: "0.8rem", fontWeight: 500 }}>
                {value}
              </span>
            </div>
          ))}
        </div>
      )}
      renderMainRow={(socket, isMobile) => (
        <>
          <AppTableCell style={{ paddingLeft: 8 }}>
            {statusDot(socket.active_state)}
            {socket.active_state}
          </AppTableCell>
          <AppTableCell>{socket.name}</AppTableCell>
          {!isMobile && (
            <>
              <AppTableCell>
                <div
                  style={{
                    display: "flex",
                    gap: theme.spacing(0.5),
                    flexWrap: "wrap",
                  }}
                >
                  {socket.listen.length > 0
                    ? socket.listen.map((addr) => (
                        <Chip
                          key={addr}
                          label={addr}
                          size="small"
                          variant="soft"
                        />
                      ))
                    : "—"}
                </div>
              </AppTableCell>
              <AppTableCell align="right">{socket.n_connections}</AppTableCell>
              <AppTableCell align="right">{socket.n_accepted}</AppTableCell>
            </>
          )}
        </>
      )}
      emptyMessage="No sockets found."
    />
  );
};

export default SocketTableView;
