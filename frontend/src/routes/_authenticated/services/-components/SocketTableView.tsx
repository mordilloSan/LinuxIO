import { useCallback } from "react";

import type { Socket } from "@/api";
import Chip from "@/components/ui/AppChip";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useAppTheme } from "@/theme";

import UnitStatusDot from "./UnitStatusDot";
import { UnitTableView } from "./UnitViews";

interface SocketTableViewProps {
  onSelect?: (name: string | null) => void;
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

const renderSocketMainRow = (socket: Socket, isMobile: boolean) => [
  <>
    <UnitStatusDot activeState={socket.active_state} />
    {socket.active_state}
  </>,
  socket.name,
  ...(isMobile
    ? []
    : [
        <SocketListenAddresses key="listen" socket={socket} />,
        socket.n_connections,
        socket.n_accepted,
      ]),
];

const SocketTableView = ({
  surface,
  sockets,
  onSelect,
}: SocketTableViewProps) => {
  const handleSelect = useCallback(
    (key: string | number | null) =>
      onSelect?.(typeof key === "string" ? key : null),
    [onSelect],
  );

  const dnd = useReorderableTableDnd<Socket, Socket>({
    handleAriaLabel: "Reorder socket",
    surface,
  });

  return (
    <UnitTableView
      dnd={dnd}
      data={sockets}
      desktopColumns={desktopColumns}
      emptyMessage="No sockets found."
      getRowKey={getSocketRowKey}
      mobileColumns={mobileColumns}
      onSelect={handleSelect}
      renderMainRow={renderSocketMainRow}
    />
  );
};

export default SocketTableView;
