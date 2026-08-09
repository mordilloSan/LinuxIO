import { useSuspenseQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

import type { Socket } from "@/api";
import { linuxio } from "@/api";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";

import {
  DetailRow,
  UnitCardActions,
  UnitCardsView,
  UnitStatusRows,
} from "./UnitViews";

interface SocketCardsViewProps {
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (socket: Socket) => ReactNode;
  sockets: Socket[];
  surface: ReorderableSurface<Socket>;
}

const SocketSummaryRows = ({ socket }: { socket: Socket }) => (
  <UnitStatusRows
    activeEnterTimestamp={socket.active_enter_timestamp}
    activeState={socket.active_state}
    inactiveEnterTimestamp={socket.inactive_enter_timestamp}
    subState={socket.sub_state}
    unitFileState={socket.unit_file_state}
  />
);

const SocketSelectedRows = ({ socket }: { socket: Socket }) => {
  const { data: info } = useSuspenseQuery({
    ...linuxio.systemd.get_unit_info({ unitName: socket.name }),
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

const SocketActionsWrapper = ({ socket }: { socket: Socket }) => {
  const { data: info } = useSuspenseQuery({
    ...linuxio.systemd.get_unit_info({ unitName: socket.name }),
    refetchInterval: 2000,
  });
  return (
    <UnitCardActions
      activeState={socket.active_state}
      info={info}
      unitFileState={socket.unit_file_state}
      unitName={socket.name}
    />
  );
};

const SocketCardsView = ({
  sockets,
  expanded,
  onExpand,
  renderDetailPanel,
  surface,
}: SocketCardsViewProps) => (
  <UnitCardsView
    emptyMessage="No sockets found."
    expanded={expanded}
    items={sockets}
    surface={surface}
    onExpand={onExpand}
    renderActions={(socket) => <SocketActionsWrapper socket={socket} />}
    renderBottomPanel={(socket) => (
      <UnitLogsCard title="Socket Logs" unitName={socket.name} />
    )}
    renderDetailPanel={renderDetailPanel}
    renderSelectedRows={(socket) => <SocketSelectedRows socket={socket} />}
    renderSummaryRows={(socket) => <SocketSummaryRows socket={socket} />}
  />
);

export default SocketCardsView;
