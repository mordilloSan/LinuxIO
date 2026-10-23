import type { ReactNode } from "react";

import type { Service, Socket, Timer, UnitInfo } from "@/api";
import Chip from "@/components/ui/AppChip";
import { getServiceStatusColor } from "@/constants/statusColors";
import type { LoaderQueryOptions } from "@/routes/-loader";
import { formatFileSize, formatSystemdUsec } from "@/utils/formatters";

import type { UnitInfoRow } from "./UnitInfoPanel";
import UnitStatusDot from "./UnitStatusDot";
import type { UnitListItem, UnitTableColumn } from "./UnitViews";

/** A label/value row in an expanded unit card. */
export type UnitCardRow = UnitInfoRow & { color?: string };

interface UnitKindConfig<T extends UnitListItem> {
  /** Label for the active state in the card ("Running" for services). */
  activeLabel?: string;
  /** Rows an expanded card shows after the shared status rows. */
  cardRows: (item: T, info: UnitInfo | undefined) => UnitCardRow[];
  /** Table cells after Status and Name; one per `extraColumns` entry. */
  extraCells: (item: T) => ReactNode[];
  extraColumns: UnitTableColumn[];
  nameWidth: string;
  /** Singular and lowercase; it is also the route's search key. */
  noun: "service" | "socket" | "timer";
  /** Rows the unit info panel adds under the unit file path. */
  panelRows?: (item: T, info: UnitInfo | undefined) => UnitInfoRow[];
  /** Extra text the list search matches besides name and description. */
  searchText?: (item: T) => string[];
}

export interface UnitKind<T extends UnitListItem> extends UnitKindConfig<T> {
  desktopColumns: UnitTableColumn[];
  matchesSearch: (item: T, search: string) => boolean;
  /** Also the id the manual card order is stored under and the view-mode key. */
  surfaceId: string;
  renderTableRow: (item: T, isMobile: boolean) => ReactNode[];
}

const statusColumn: UnitTableColumn = {
  field: "status",
  headerName: "Status",
  align: "left",
  width: "120px",
  style: { paddingLeft: 8 },
};

export const UNIT_MOBILE_COLUMNS: UnitTableColumn[] = [
  { ...statusColumn, width: "110px" },
  { field: "name", headerName: "Name", align: "left" },
];

function defineUnitKind<T extends UnitListItem>(
  config: UnitKindConfig<T>,
): UnitKind<T> {
  return {
    ...config,
    desktopColumns: [
      statusColumn,
      {
        field: "name",
        headerName: "Name",
        align: "left",
        width: config.nameWidth,
      },
      ...config.extraColumns,
    ],
    matchesSearch: (item, search) =>
      [
        item.name,
        item.description ?? "",
        ...(config.searchText?.(item) ?? []),
      ].some((text) => text.toLowerCase().includes(search)),
    renderTableRow: (item, isMobile) => [
      <>
        <UnitStatusDot activeState={item.active_state} />
        {item.active_state}
      </>,
      item.name,
      ...(isMobile ? [] : config.extraCells(item)),
    ],
    surfaceId: `${config.noun}s.list`,
  };
}

/** Route-context queries a unit list route loads: the list and, when open, one unit. */
export const unitListQueries = (context: {
  listQueryOptions: LoaderQueryOptions;
  selectedQueryOptions: LoaderQueryOptions | undefined;
}): LoaderQueryOptions[] =>
  context.selectedQueryOptions
    ? [context.listQueryOptions, context.selectedQueryOptions]
    : [context.listQueryOptions];

const loadRow = (unit: UnitListItem): UnitCardRow => ({
  label: "Load",
  value: unit.load_state,
});

export const serviceKind = defineUnitKind<Service>({
  activeLabel: "Running",
  cardRows: (service, info) => {
    const mainPid = info?.MainPID ?? 0;
    const memory = info?.MemoryCurrent ?? 0;
    return [
      {
        label: "Active",
        value: service.active_state,
        color: getServiceStatusColor(service.active_state),
      },
      {
        ...loadRow(service),
        color:
          service.load_state === "loaded" ? "text.primary" : "text.secondary",
      },
      { label: "PID", value: mainPid, hidden: mainPid <= 0 },
      {
        label: "Memory",
        value: formatFileSize(memory, 1),
        // systemd reports "unset" as a UINT64_MAX-sized value.
        hidden: !memory || memory > 1e18,
      },
    ];
  },
  extraCells: (service) => [
    service.load_state,
    service.sub_state,
    service.description || "-",
  ],
  extraColumns: [
    {
      field: "load_state",
      headerName: "Load State",
      align: "left",
      width: "120px",
    },
    {
      field: "sub_state",
      headerName: "Sub State",
      align: "left",
      width: "120px",
    },
    { field: "description", headerName: "Description", align: "left" },
  ],
  nameWidth: "200px",
  noun: "service",
});

const socketRows = (socket: Socket, info: UnitInfo | undefined) => {
  const listen = Array.isArray(info?.Listen) ? info.Listen : socket.listen;
  return [
    { label: "Listen", value: listen.length > 0 ? listen.join(", ") : "—" },
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
};

export const socketKind = defineUnitKind<Socket>({
  cardRows: (socket, info) => [loadRow(socket), ...socketRows(socket, info)],
  extraCells: (socket) => [
    <div
      key="listen"
      style={{ display: "flex", gap: "var(--app-space-2)", flexWrap: "wrap" }}
    >
      {socket.listen.length > 0
        ? socket.listen.map((address) => (
            <Chip key={address} label={address} size="small" variant="soft" />
          ))
        : "—"}
    </div>,
    socket.n_connections,
    socket.n_accepted,
  ],
  extraColumns: [
    { field: "listen", headerName: "Listen", align: "left" },
    {
      field: "connections",
      headerName: "Connections",
      align: "right",
      width: "130px",
    },
    {
      field: "accepted",
      headerName: "Accepted",
      align: "right",
      width: "120px",
    },
  ],
  nameWidth: "220px",
  noun: "socket",
  panelRows: socketRows,
  searchText: (socket) => socket.listen,
});

const timerRows = (timer: Timer, info: UnitInfo | undefined) => [
  {
    label: "Unit",
    value: info?.Unit ?? timer.unit ?? "—",
    hidden: !info && !timer.unit,
  },
  {
    label: "Next",
    value: formatSystemdUsec(info?.NextElapseUSec ?? timer.next_elapse_usec),
  },
  {
    label: "Last",
    value: formatSystemdUsec(info?.LastTriggerUSec ?? timer.last_trigger_usec),
  },
];

export const timerKind = defineUnitKind<Timer>({
  cardRows: (timer, info) => [loadRow(timer), ...timerRows(timer, info)],
  extraCells: (timer) => [
    timer.unit || "—",
    formatSystemdUsec(timer.next_elapse_usec),
    formatSystemdUsec(timer.last_trigger_usec),
  ],
  extraColumns: [
    {
      field: "unit",
      headerName: "Unit",
      align: "left",
      width: "220px",
    },
    {
      field: "next_elapse",
      headerName: "Next Elapse",
      align: "left",
      width: "180px",
    },
    { field: "last_trigger", headerName: "Last Trigger", align: "left" },
  ],
  nameWidth: "220px",
  noun: "timer",
  panelRows: timerRows,
  searchText: (timer) => [timer.unit],
});
