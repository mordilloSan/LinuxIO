import React from "react";

import { AutoStartRow, DetailRow, UnitCardsView } from "./UnitViews";

import type { Socket } from "@/api";
import { linuxio } from "@/api";
import { getServiceStatusColor } from "@/constants/statusColors";

interface SocketCardsViewProps {
  sockets: Socket[];
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (socket: Socket) => React.ReactNode;
}

const SocketSummaryRows: React.FC<{ socket: Socket }> = ({ socket }) => {
  const statusColor = getServiceStatusColor(socket.active_state);

  return (
    <>
      <DetailRow label="Status" noBorder>
        <span
          style={{
            fontSize: "0.85rem",
            fontWeight: 600,
            color: statusColor,
          }}
        >
          {socket.active_state}
          {socket.sub_state && socket.sub_state !== socket.active_state && (
            <span
              style={{
                color: "var(--mui-palette-text-secondary)",
                marginLeft: 8,
                fontWeight: 400,
              }}
            >
              ({socket.sub_state})
            </span>
          )}
        </span>
      </DetailRow>
      <AutoStartRow unitFileState={socket.unit_file_state} />
    </>
  );
};

const SocketSelectedRows: React.FC<{ socket: Socket }> = ({ socket }) => {
  const { data: info } = linuxio.dbus.get_unit_info.useQuery(socket.name, {
    refetchInterval: 2000,
  });
  const listen = Array.isArray(info?.Listen) ? info.Listen : socket.listen;

  return (
    <>
      <DetailRow label="Load">
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
          {socket.load_state}
        </span>
      </DetailRow>
      <DetailRow label="Listen">
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
          {listen.length > 0 ? listen.join(", ") : "—"}
        </span>
      </DetailRow>
      <DetailRow label="Connections">
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
          {String(info?.NConnections ?? socket.n_connections)}
        </span>
      </DetailRow>
      <DetailRow label="Accepted">
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
          {String(info?.NAccepted ?? socket.n_accepted)}
        </span>
      </DetailRow>
    </>
  );
};

const SocketCardsView: React.FC<SocketCardsViewProps> = ({
  sockets,
  expanded,
  onExpand,
  renderDetailPanel,
}) => (
  <UnitCardsView
    items={sockets}
    expanded={expanded}
    onExpand={onExpand}
    emptyMessage="No sockets found."
    renderSummaryRows={(socket) => <SocketSummaryRows socket={socket} />}
    renderSelectedRows={(socket) => <SocketSelectedRows socket={socket} />}
    renderDetailPanel={renderDetailPanel}
  />
);

export default SocketCardsView;
