import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import type { Socket, TableCardViewMode, UnitInfo } from "@/api";

import SocketCardsView from "./SocketCardsView";
import SocketTableView from "./SocketTableView";
import UnitListTab from "./UnitListTab";
import { UnitInfoPanel } from "./UnitViews";

function compareSocketsByName(a: Socket, b: Socket): number {
  return a.name.localeCompare(b.name, undefined, {
    numeric: true,
    sensitivity: "base",
  });
}

function matchesSocketSearch(socket: Socket, search: string): boolean {
  return (
    socket.name.toLowerCase().includes(search) ||
    (socket.description?.toLowerCase().includes(search) ?? false) ||
    socket.listen.some((address) => address.toLowerCase().includes(search))
  );
}

function useSocketsQuery(viewMode: TableCardViewMode) {
  return useSuspenseQuery({
    ...linuxio.systemd.list_sockets,
    refetchInterval: viewMode === "card" ? false : 5000,
  });
}

function buildSocketInfoRows(socket: Socket, info: UnitInfo | undefined) {
  const listen = Array.isArray(info?.Listen)
    ? info.Listen.join(", ")
    : socket.listen.join(", ");

  return [
    {
      label: "Listen",
      value: listen || "—",
    },
    {
      label: "Connections",
      value: String(info?.NConnections ?? socket.n_connections),
      hidden: !info && socket.n_connections === 0,
    },
    {
      label: "Accepted",
      value: String(info?.NAccepted ?? socket.n_accepted),
      hidden: !info && socket.n_accepted === 0,
    },
  ];
}

interface SocketsTabProps {
  onSelectedChange: (name: string | null) => void;
  selected?: string;
  viewMode: TableCardViewMode;
}

const SocketsTab = ({
  onSelectedChange,
  selected,
  viewMode,
}: SocketsTabProps) => {
  const { data } = useSocketsQuery(viewMode);

  return (
    <UnitListTab
      compareItems={compareSocketsByName}
      data={data}
      matchesSearch={matchesSocketSearch}
      onSelectedChange={onSelectedChange}
      renderCardsView={({
        items,
        expanded,
        onExpand,
        renderDetailPanel,
        surface,
      }) => (
        <SocketCardsView
          expanded={expanded}
          onExpand={onExpand}
          renderDetailPanel={renderDetailPanel}
          sockets={items}
          surface={surface}
        />
      )}
      renderDetailPanel={(socket, onClose) => (
        <UnitInfoPanel
          onClose={onClose}
          renderInfoRows={(info) => buildSocketInfoRows(socket, info)}
          unitName={socket.name}
        />
      )}
      renderTableView={({ items, onSelect, surface }) => (
        <SocketTableView
          onSelect={onSelect}
          sockets={items}
          surface={surface}
        />
      )}
      searchPlaceholder="Search sockets…"
      selected={selected}
      surfaceId="sockets.list"
      viewMode={viewMode}
    />
  );
};

export default SocketsTab;
