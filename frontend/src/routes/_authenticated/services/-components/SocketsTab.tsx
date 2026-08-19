import { useSuspenseQuery } from "@tanstack/react-query";

import type { linuxio } from "@/api";
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

function useSocketsQuery(
  listQueryOptions: typeof linuxio.systemd.list_sockets,
  viewMode: TableCardViewMode,
) {
  return useSuspenseQuery({
    ...listQueryOptions,
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
  listQueryOptions: typeof linuxio.systemd.list_sockets;
  onSelectedChange: (name: string | null) => void;
  selected?: string;
  selectedQueryOptions:
    | ReturnType<typeof linuxio.systemd.get_unit_info>
    | undefined;
  viewMode: TableCardViewMode;
}

const SocketsTab = ({
  listQueryOptions,
  onSelectedChange,
  selected,
  selectedQueryOptions,
  viewMode,
}: SocketsTabProps) => {
  const { data } = useSocketsQuery(listQueryOptions, viewMode);

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
          queryOptions={
            selected === socket.name ? selectedQueryOptions : undefined
          }
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
