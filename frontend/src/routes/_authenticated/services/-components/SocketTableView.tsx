import { useCallback } from "react";

import type { Socket } from "@/api";
import Chip from "@/components/ui/AppChip";
import { AppTableCell } from "@/components/ui/AppTable";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import { useVirtualReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useAppTheme } from "@/theme";

import UnitStatusDot from "./UnitStatusDot";
import { MobileExpandedDetails, UnitTableView } from "./UnitViews";

interface SocketTableViewProps {
  onDoubleClick?: (name: string) => void;
  onSelect?: (name: string | null) => void;
  selected?: string | null;
  sockets: Socket[];
  surface: ReorderableSurface<Socket>;
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

const getSocketRowKey = (socket: Socket) => socket.name;

function SocketListenAddresses({ socket }: { socket: Socket }) {
  const theme = useAppTheme();

  return (
    <div
      style={{
        display: "flex",
        gap: theme.spacing(0.5),
        flexWrap: "wrap",
      }}
    >
      {socket.listen.length > 0
        ? socket.listen.map((addr) => (
            <Chip key={addr} label={addr} size="small" variant="soft" />
          ))
        : "—"}
    </div>
  );
}

const renderSocketMainRow = (socket: Socket, isMobile: boolean) => (
  <>
    <AppTableCell style={{ paddingLeft: 8 }}>
      <UnitStatusDot activeState={socket.active_state} />
      {socket.active_state}
    </AppTableCell>
    <AppTableCell>{socket.name}</AppTableCell>
    {!isMobile && (
      <>
        <AppTableCell>
          <SocketListenAddresses socket={socket} />
        </AppTableCell>
        <AppTableCell align="right">{socket.n_connections}</AppTableCell>
        <AppTableCell align="right">{socket.n_accepted}</AppTableCell>
      </>
    )}
  </>
);

const renderSocketMobileExpandedContent = (socket: Socket) => (
  <MobileExpandedDetails
    rows={[
      { label: "Listen", value: socket.listen.join(", ") || "—" },
      { label: "Connections", value: String(socket.n_connections) },
      { label: "Accepted", value: String(socket.n_accepted) },
    ]}
  />
);

const SocketTableView = ({
  surface,
  sockets,
  selected,
  onSelect,
  onDoubleClick,
}: SocketTableViewProps) => {
  const handleDoubleClick = useCallback(
    (key: string | number) => {
      if (typeof key === "string") {
        onDoubleClick?.(key);
      }
    },
    [onDoubleClick],
  );
  const handleSelect = useCallback(
    (key: string | number | null) =>
      onSelect?.(typeof key === "string" ? key : null),
    [onSelect],
  );

  const dnd = useVirtualReorderableTableDnd<Socket, Socket>({ surface });

  return (
    <UnitTableView
      dnd={dnd}
      data={sockets}
      desktopColumns={desktopColumns}
      emptyMessage="No sockets found."
      getRowKey={getSocketRowKey}
      mobileColumns={mobileColumns}
      onDoubleClick={handleDoubleClick}
      onSelect={handleSelect}
      renderMainRow={renderSocketMainRow}
      renderMobileExpandedContent={renderSocketMobileExpandedContent}
      selected={selected}
    />
  );
};

export default SocketTableView;
