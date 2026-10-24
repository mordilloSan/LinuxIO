import type { Row } from "@tanstack/react-table";
import {
  lazy,
  memo,
  Suspense,
  useCallback,
  useMemo,
  useState,
  type MouseEvent,
} from "react";

import type { ContainerInfo } from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type {
  AppVirtualTableDndOptions,
  AppVirtualTableRowAttributes,
  AppVirtualTableRowRenderProps,
} from "@/components/tables/AppVirtualTable";
import type {
  AppVirtualTableColumnDef,
  AppTableFeatures,
} from "@/components/tables/AppVirtualTable.types";
import { clickTargetsRowBody } from "@/components/tables/rowInteraction";
import AppCollapse from "@/components/ui/AppCollapse";
import AppTooltip from "@/components/ui/AppTooltip";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { down } from "@/theme/breakpoints";
import {
  getContainerDisplayState,
  getContainerMounts,
  getContainerName,
  getDedupedPorts,
} from "@/utils/dockerContainer";
import { formatFileSize } from "@/utils/formatters";

import {
  buildContainerTableRows,
  getComposeProject,
  isStackHeaderRow,
  type ContainerTableRow,
} from "./containerStacks";
import {
  ActionsCell,
  CheckingUpdatesContext,
  ContainerNameCell,
  ExpandedContainersContext,
  MetricsCell,
  NetworkAddressCell,
  NetworkCell,
  PortsCell,
  StackHeaderCell,
  UpdateCell,
  UptimeCell,
  VersionCell,
  VolumesCell,
} from "./ContainerTableCells";
import { containerTableCollisionDetection } from "./containerTableDnd";
import {
  areContainerArraysEquivalent,
  areStringSetsEqual,
  asContainer,
  containerCellRenderKey,
  getContainerTableRowId,
  getVersionDisplay,
} from "./containerTableModel";

import "./container-table.css";

const LogsDialog = lazy(() => import("@/components/docker/LogsDialog"));
const TerminalDialog = lazy(() => import("@/components/docker/TerminalDialog"));

const EMPTY_STOPPING_CONTAINER_IDS = new Set<string>();

interface ContainerTableProps {
  checkingUpdates?: boolean;
  /** Stacks folded down to their header row. Owned by the page, so the fold
   * survives the card/table toggle. */
  collapsedStackIds?: ReadonlySet<string>;
  containers: ContainerInfo[];
  /** Reorder wiring from `useReorderableTableDnd`; omit to lock the row order. */
  dnd?: AppVirtualTableDndOptions<ContainerTableRow>;
  onSelectContainer?: (containerId: string) => void;
  onToggleStack?: (project: string) => void;
  stoppingContainerIds?: ReadonlySet<string>;
}

interface ContainerDialogTarget {
  id: string;
  name: string;
}

const EMPTY_COLLAPSED_STACK_IDS = new Set<string>();

const ContainerTable = ({
  checkingUpdates = false,
  collapsedStackIds = EMPTY_COLLAPSED_STACK_IDS,
  containers,
  dnd,
  onSelectContainer,
  onToggleStack,
  stoppingContainerIds = EMPTY_STOPPING_CONTAINER_IDS,
}: ContainerTableProps) => {
  // Same breakpoint the Version and Uptime columns hide at, so the strip
  // collapses exactly when Actions starts crowding the name.
  const compactActions = useMediaQuery(down("md"));
  const editMode = dnd?.editing ?? false;
  // Table mode keeps the same stack entries as card mode even while editing:
  // headers move whole blocks, while their visible member rows stay inert.
  const rows = useMemo<ContainerTableRow[]>(
    () => buildContainerTableRows(containers, collapsedStackIds),
    [collapsedStackIds, containers],
  );
  const {
    collapsedStackMemberIds,
    stackGroupIdsByMemberId,
    stackMemberIds,
    stackMemberRowClasses,
  } = useMemo(() => {
    const collapsedMemberIds = new Set<string>();
    const groupIdsByMemberId = new Map<string, string>();
    const memberIds = new Set<string>();
    const memberRowClasses = new Map<string, string>();
    for (const row of rows) {
      if (isStackHeaderRow(row)) {
        const groupId = getContainerTableRowId(row);
        for (const [index, member] of row.containers.entries()) {
          memberIds.add(member.Id);
          groupIdsByMemberId.set(member.Id, groupId);
          if (row.collapsed) collapsedMemberIds.add(member.Id);
          memberRowClasses.set(
            member.Id,
            [
              "container-table__stack-member-row",
              index === 0 && "container-table__stack-member-row--first",
              index === row.containers.length - 1 &&
                "container-table__stack-member-row--last",
            ]
              .filter(Boolean)
              .join(" "),
          );
        }
      }
    }
    return {
      collapsedStackMemberIds: collapsedMemberIds,
      stackGroupIdsByMemberId: groupIdsByMemberId,
      stackMemberIds: memberIds,
      stackMemberRowClasses: memberRowClasses,
    };
  }, [rows]);
  const sortableRowIds = useMemo(
    () =>
      rows
        .filter((row) => isStackHeaderRow(row) || !stackMemberIds.has(row.Id))
        .map(getContainerTableRowId),
    [rows, stackMemberIds],
  );
  const hasStackGroups = stackMemberIds.size > 0;
  const stackAwareDnd = useMemo(
    () =>
      dnd
        ? {
            ...dnd,
            contextProps: {
              ...dnd.contextProps,
              collisionDetection: hasStackGroups
                ? containerTableCollisionDetection
                : dnd.contextProps.collisionDetection,
            },
            // `itemIds` contains loose containers and stack headers only.
            // Group members still render underneath their header but never
            // register with dnd-kit, so a stack can move only as one block.
            getSortableGroupId: (
              row: Row<AppTableFeatures, ContainerTableRow>,
            ) =>
              isStackHeaderRow(row.original)
                ? getContainerTableRowId(row.original)
                : stackGroupIdsByMemberId.get(row.original.Id),
            isRowSortable: (row: Row<AppTableFeatures, ContainerTableRow>) =>
              sortableRowIds.includes(String(dnd.getItemId(row))),
            itemIds: sortableRowIds,
          }
        : undefined,
    [dnd, hasStackGroups, sortableRowIds, stackGroupIdsByMemberId],
  );
  const getRowAttributes = useCallback(
    (
      row: Row<AppTableFeatures, ContainerTableRow>,
    ): AppVirtualTableRowAttributes => ({
      className: !isStackHeaderRow(row.original)
        ? stackMemberRowClasses.get(row.original.Id)
        : undefined,
    }),
    [stackMemberRowClasses],
  );
  const renderRow = useCallback(
    ({
      cells,
      dragHandle,
      row,
      rowProps,
    }: AppVirtualTableRowRenderProps<ContainerTableRow>) => {
      const original = row.original;
      if (isStackHeaderRow(original)) {
        return (
          <div
            {...rowProps}
            aria-expanded={!original.collapsed}
            className={[rowProps.className, "container-table__stack-group-row"]
              .filter(Boolean)
              .join(" ")}
          >
            <StackHeaderCell
              dragHandle={dragHandle}
              header={original}
              onToggleStack={onToggleStack}
            />
          </div>
        );
      }
      if (!stackMemberIds.has(original.Id)) {
        return <div {...rowProps}>{cells}</div>;
      }
      return (
        <AppCollapse
          in={!collapsedStackMemberIds.has(original.Id)}
          unmountOnExit
        >
          <div {...rowProps}>{cells}</div>
        </AppCollapse>
      );
    },
    [collapsedStackMemberIds, onToggleStack, stackMemberIds],
  );
  const [expandedContainerIds, setExpandedContainerIds] = useState<Set<string>>(
    () => new Set(),
  );
  const [logsTarget, setLogsTarget] = useState<ContainerDialogTarget | null>(
    null,
  );
  const [logsDialogOpen, setLogsDialogOpen] = useState(false);
  const [terminalTarget, setTerminalTarget] =
    useState<ContainerDialogTarget | null>(null);
  const [terminalDialogOpen, setTerminalDialogOpen] = useState(false);
  const toggleExpanded = useCallback((containerId: string) => {
    setExpandedContainerIds((previous) => {
      const next = new Set(previous);
      if (next.has(containerId)) {
        next.delete(containerId);
      } else {
        next.add(containerId);
      }
      return next;
    });
  }, []);
  const openLogs = useCallback((containerId: string, containerName: string) => {
    setLogsTarget({ id: containerId, name: containerName });
    setLogsDialogOpen(true);
  }, []);
  const openTerminal = useCallback(
    (containerId: string, containerName: string) => {
      setTerminalTarget({ id: containerId, name: containerName });
      setTerminalDialogOpen(true);
    },
    [],
  );
  const handleRowClick = useCallback(
    (row: { original: ContainerTableRow }, event: MouseEvent) => {
      // A row is a container of controls before it is a link: the action
      // buttons, the "+N more" expander, the update dot and the copy-on-click
      // cells all bubble up to here and must not navigate.
      if (!clickTargetsRowBody(event.target)) return;
      if (isStackHeaderRow(row.original)) {
        onToggleStack?.(row.original.project);
        return;
      }
      onSelectContainer?.(row.original.Id);
    },
    [onSelectContainer, onToggleStack],
  );
  const columns = useMemo<AppVirtualTableColumnDef<ContainerTableRow>[]>(
    () => [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <ContainerNameCell container={asContainer(row.original)} />
        ),
        meta: {
          align: "left",
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getContainerName(container),
            getContainerDisplayState(container),
            container.icon,
          ]),
          width: "minmax(0, 1.6fr)",
        },
      },
      {
        id: "version",
        header: "Version",
        cell: ({ row }) => {
          const container = asContainer(row.original);
          return (
            <div className="container-table__version-stack">
              <VersionCell version={getVersionDisplay(container)} />
              <UpdateCell
                containerId={container.Id}
                name={getContainerName(container)}
                state={container.State}
                updateAvailable={container.updateAvailable}
                updateCheckedAt={container.updateCheckedAt}
                updateCheckReason={container.updateCheckReason}
                updateCheckState={container.updateCheckState}
                updateError={container.updateError}
              />
            </div>
          );
        },
        meta: {
          align: "center",
          // No `checkingUpdates` here for the same reason the actions column
          // omits the auto-update state: the cell subscribes to it directly.
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getVersionDisplay(container),
            getContainerName(container),
            container.updateAvailable,
            container.updateCheckedAt,
            container.updateCheckReason,
            container.updateCheckState,
            container.updateError,
          ]),
          hideBelow: "md",
          width: "170px",
        },
      },
      {
        id: "uptime",
        header: "Uptime",
        cell: ({ row }) => (
          <UptimeCell created={asContainer(row.original).Created} />
        ),
        meta: { hideBelow: "md", width: "90px" },
      },
      {
        id: "network",
        header: "Network",
        cell: ({ row }) => (
          <NetworkCell
            networks={Object.entries(
              asContainer(row.original).NetworkSettings?.Networks ?? {},
            )}
          />
        ),
        meta: {
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            Object.keys(container.NetworkSettings?.Networks ?? {})
              .sort()
              .join("|"),
          ]),
          hideBelow: "lg",
        },
      },
      {
        id: "ip",
        header: "Container IP",
        cell: ({ row }) => (
          <NetworkAddressCell
            networks={Object.entries(
              asContainer(row.original).NetworkSettings?.Networks ?? {},
            )}
          />
        ),
        meta: {
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            Object.entries(container.NetworkSettings?.Networks ?? {})
              .sort(([left], [right]) => left.localeCompare(right))
              .map(
                ([networkName, endpoint]) =>
                  `${networkName}:${endpoint.IPAddress || "-"}`,
              )
              .join("|"),
          ]),
          hideBelow: "lg",
          width: "130px",
        },
      },
      {
        id: "ports",
        header: () => (
          <AppTooltip placement="top" title="Container -> Host">
            <span>Ports</span>
          </AppTooltip>
        ),
        cell: ({ row }) => {
          const container = asContainer(row.original);
          return (
            <PortsCell
              containerId={container.Id}
              onToggleExpanded={toggleExpanded}
              ports={getDedupedPorts(container)}
            />
          );
        },
        meta: {
          hideBelow: "xl",
          width: "minmax(130px, 155px)",
          cellStyle: { alignItems: "flex-start" },
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getDedupedPorts(container)
              .map(
                (port) =>
                  `${port.PrivatePort}:${port.PublicPort ?? "-"}:${port.Type}`,
              )
              .join("|"),
          ]),
        },
      },
      {
        id: "volumes",
        header: () => (
          <AppTooltip placement="top" title="App -> Host">
            <span>Volumes</span>
          </AppTooltip>
        ),
        cell: ({ row }) => {
          const container = asContainer(row.original);
          return (
            <VolumesCell
              containerId={container.Id}
              mounts={getContainerMounts(container)}
              onToggleExpanded={toggleExpanded}
            />
          );
        },
        meta: {
          hideBelow: "xl",
          width: "minmax(0, 2.2fr)",
          cellStyle: { alignItems: "flex-start" },
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getContainerMounts(container)
              .map(
                (mount) => `${mount.Type}:${mount.Destination}:${mount.Source}`,
              )
              .join("|"),
          ]),
        },
      },
      {
        id: "metrics",
        header: "CPU / Mem",
        cell: ({ row }) => (
          <MetricsCell container={asContainer(row.original)} />
        ),
        meta: {
          align: "center",
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            container.metrics?.status ?? "",
            container.metrics?.cpu_percent?.toFixed(1) ?? "",
            container.metrics?.memory_usage_bytes === undefined
              ? ""
              : formatFileSize(container.metrics.memory_usage_bytes),
          ]),
          hideBelow: "xl",
          width: "110px",
        },
      },
      {
        id: "actions",
        header: "Actions",
        enableSorting: false,
        cell: ({ row }) => {
          const container = asContainer(row.original);
          const name = getContainerName(container);
          return (
            <ActionsCell
              compact={compactActions}
              containerId={container.Id}
              labels={container.Labels}
              name={name}
              onOpenLogs={openLogs}
              onOpenTerminal={openTerminal}
              pending={stoppingContainerIds.has(container.Id)}
              state={container.State}
              url={container.url}
            />
          );
        },
        meta: {
          align: "right",
          // The buttons fill the column, so a right-aligned label reads as
          // hanging off the end of the strip rather than titling it.
          headerStyle: { justifyContent: "center" },
          // Tighter than the default 16px so the action strip fits the track.
          cellStyle: { gap: 2, paddingInline: 8 },
          // A compact row holds nothing but the menu button, so the rest of the
          // track goes back to the name.
          width: compactActions ? "56px" : "76px",
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getContainerName(container),
            container.State,
            container.url,
            getComposeProject(container),
            stoppingContainerIds.has(container.Id),
          ]),
        },
      },
    ],
    [
      compactActions,
      openLogs,
      openTerminal,
      stoppingContainerIds,
      toggleExpanded,
    ],
  );
  return (
    <>
      <ExpandedContainersContext.Provider value={expandedContainerIds}>
        <CheckingUpdatesContext.Provider value={checkingUpdates}>
          <AppVirtualTable
            ariaLabel="Docker containers"
            columns={columns}
            data={rows}
            dnd={stackAwareDnd}
            emptyMessage="No containers found."
            enableSorting={false}
            fillAvailable
            getRowAttributes={getRowAttributes}
            getRowId={getContainerTableRowId}
            // Dragging rows is the point of edit mode; selecting one there
            // would fight the drag and immediately swap the table for a
            // detail view.
            onRowClick={
              (onSelectContainer || onToggleStack) && !editMode
                ? handleRowClick
                : undefined
            }
            renderRow={renderRow}
          />
        </CheckingUpdatesContext.Provider>
      </ExpandedContainersContext.Provider>
      <Suspense fallback={null}>
        {logsTarget && (
          <LogsDialog
            containerId={logsTarget.id}
            containerName={logsTarget.name}
            onClose={() => setLogsDialogOpen(false)}
            onExited={() => setLogsTarget(null)}
            open={logsDialogOpen}
          />
        )}
        {terminalTarget && (
          <TerminalDialog
            containerId={terminalTarget.id}
            containerName={terminalTarget.name}
            onClose={() => setTerminalDialogOpen(false)}
            onExited={() => setTerminalTarget(null)}
            open={terminalDialogOpen}
          />
        )}
      </Suspense>
    </>
  );
};

const areContainerTablePropsEqual = (
  previous: ContainerTableProps,
  next: ContainerTableProps,
) =>
  previous.checkingUpdates === next.checkingUpdates &&
  previous.dnd === next.dnd &&
  previous.onSelectContainer === next.onSelectContainer &&
  previous.onToggleStack === next.onToggleStack &&
  areStringSetsEqual(
    previous.collapsedStackIds ?? EMPTY_COLLAPSED_STACK_IDS,
    next.collapsedStackIds ?? EMPTY_COLLAPSED_STACK_IDS,
  ) &&
  areStringSetsEqual(
    previous.stoppingContainerIds ?? EMPTY_STOPPING_CONTAINER_IDS,
    next.stoppingContainerIds ?? EMPTY_STOPPING_CONTAINER_IDS,
  ) &&
  areContainerArraysEquivalent(previous.containers, next.containers);

export default memo(ContainerTable, areContainerTablePropsEqual);
