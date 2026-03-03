import React from "react";

import UnitLogsCard from "./UnitLogsCard";
import {
  DetailRow,
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
} from "./UnitViews";

import type { Socket } from "@/api";
import { linuxio } from "@/api";

interface SocketCardsViewProps {
  sockets: Socket[];
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (socket: Socket) => React.ReactNode;
}

const SocketSummaryRows: React.FC<{ socket: Socket }> = ({ socket }) => (
  <UnitStatusRows
    activeState={socket.active_state}
    subState={socket.sub_state}
    unitFileState={socket.unit_file_state}
    activeEnterTimestamp={socket.active_enter_timestamp}
    inactiveEnterTimestamp={socket.inactive_enter_timestamp}
  />
);

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

const SocketActionsWrapper: React.FC<{ socket: Socket }> = ({ socket }) => {
  const { data: info } = linuxio.dbus.get_unit_info.useQuery(socket.name, {
    refetchInterval: 2000,
  });
  return (
    <UnitCardActions
      unitName={socket.name}
      activeState={socket.active_state}
      unitFileState={socket.unit_file_state}
      info={info}
    />
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
    renderActions={(socket) => <SocketActionsWrapper socket={socket} />}
    renderDetailPanel={renderDetailPanel}
    renderBottomPanel={(socket) => (
      <UnitLogsCard unitName={socket.name} title="Socket Logs" />
    )}
  />
);

export default SocketCardsView;
