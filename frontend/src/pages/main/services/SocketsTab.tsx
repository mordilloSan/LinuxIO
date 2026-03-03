import React from "react";

import SocketCardsView from "./SocketCardsView";
import SocketTableView from "./SocketTableView";
import UnitListTab from "./UnitListTab";
import { UnitInfoPanel } from "./UnitViews";

import { linuxio } from "@/api";
import type { Socket, UnitInfo } from "@/api";
import { useViewMode } from "@/hooks/useViewMode";
import type { TableCardViewMode } from "@/types/config";

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
  return linuxio.dbus.list_sockets.useQuery({
    refetchInterval: viewMode === "card" ? false : 5000,
  });
}

function buildSocketInfoRows(
  socket: Socket,
  info: UnitInfo | undefined,
  isPending: boolean,
) {
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
      hidden: isPending && !info && socket.n_connections === 0,
    },
    {
      label: "Accepted",
      value: String(info?.NAccepted ?? socket.n_accepted),
      hidden: isPending && !info && socket.n_accepted === 0,
    },
  ];
}

const SocketsTab: React.FC = () => {
  const [viewMode, setViewMode] = useViewMode("sockets.list", "table");
  const { data, isPending, isError, error } = useSocketsQuery(viewMode);

  return (
    <UnitListTab
      viewMode={viewMode}
      setViewMode={setViewMode}
      data={data}
      isPending={isPending}
      isError={isError}
      error={error}
      searchPlaceholder="Search sockets…"
      errorMessage="Failed to load sockets"
      compareItems={compareSocketsByName}
      matchesSearch={matchesSocketSearch}
      renderTableView={({ items, selected, onSelect, onDoubleClick }) => (
        <SocketTableView
          sockets={items}
          selected={selected}
          onSelect={onSelect}
          onDoubleClick={onDoubleClick}
        />
      )}
      renderCardsView={({ items, expanded, onExpand, renderDetailPanel }) => (
        <SocketCardsView
          sockets={items}
          expanded={expanded}
          onExpand={onExpand}
          renderDetailPanel={renderDetailPanel}
        />
      )}
      renderDetailPanel={(socket, onClose) => (
        <UnitInfoPanel
          unitName={socket.name}
          onClose={onClose}
          renderInfoRows={(info, isPending) =>
            buildSocketInfoRows(socket, info, isPending)
          }
        />
      )}
    />
  );
};

export default SocketsTab;
