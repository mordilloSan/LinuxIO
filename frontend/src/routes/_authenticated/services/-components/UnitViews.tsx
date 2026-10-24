import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import type { RowData } from "@tanstack/react-table";
import {
  useCallback,
  useMemo,
  type CSSProperties,
  type ReactNode,
} from "react";

import type { UnitInfo } from "@/api";
import { linuxio, useCallMutation } from "@/api";
import ResourceDetailsLayout from "@/components/cards/ResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import type { ReorderableSurface } from "@/components/reorder/useReorderableSurface";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableDndOptions } from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { DetailRow } from "@/components/ui/InfoRow";
import { getServiceStatusColor } from "@/constants/statusColors";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import UnitCard from "@/routes/_authenticated/services/-components/UnitCard";
import type { UnitListItem } from "@/routes/_authenticated/services/-components/UnitCard";
import { down } from "@/theme/breakpoints";
import { formatSystemdUsec } from "@/utils/formatters";

import { formatUnitFileState } from "./unitFormatters";

export type { UnitListItem } from "@/routes/_authenticated/services/-components/UnitCard";

const SHORT_DATE_TIME: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
};

// In layout mode a card press belongs to the drag, not to opening the unit.
const noopExpand = () => {};
const getUnitId = (item: UnitListItem) => item.name;

const UNIT_CARD_MIN_WIDTH = 360;
const UNIT_CARD_ESTIMATE_HEIGHT = 150;
const SERVICES_TOAST_META = {
  label: "Open services",
  to: "/services",
} as const;

interface UnitTableViewProps<T extends RowData> {
  data: T[];
  dnd?: AppVirtualTableDndOptions<T>;
  desktopColumns: UnitTableColumn[];
  emptyMessage: string;
  getRowKey: (row: T, index: number) => string | number;
  mobileColumns: UnitTableColumn[];
  onSelect?: (key: string | number | null) => void;
  /** One node per column of the active `desktopColumns`/`mobileColumns` set. */
  renderMainRow: (row: T, isMobile: boolean, index: number) => ReactNode[];
}

export interface UnitTableColumn {
  align?: "left" | "center" | "right";
  field: string;
  headerName: string;
  style?: CSSProperties;
  width?: string | number;
}

interface UnitCardsViewProps<T extends UnitListItem> {
  emptyMessage: string;
  surface: ReorderableSurface<T>;
  expanded: string | null;
  items: T[];
  onExpand: (name: string | null) => void;
  /** `info` is the shared `get_unit_info` result, fetched once per expanded card. */
  renderActions?: (item: T, info: UnitInfo | undefined) => ReactNode;
  renderBottomPanel?: (item: T) => ReactNode;
  renderDetailPanel: (item: T) => ReactNode;
  renderSelectedRows?: (item: T, info: UnitInfo | undefined) => ReactNode;
  renderSummaryRows: (item: T) => ReactNode;
}

/**
 * Mounted only for the single expanded card. Fetches `get_unit_info` once and
 * hands the result to both `renderSelectedRows` and `renderActions`, instead
 * of each render prop polling the same query key with its own observer.
 */
function ExpandedUnitCard<T extends UnitListItem>({
  item,
  onExpand,
  renderSummaryRows,
  renderSelectedRows,
  renderActions,
}: {
  item: T;
  onExpand: (name: string | null) => void;
  renderActions?: (item: T, info: UnitInfo | undefined) => ReactNode;
  renderSelectedRows?: (item: T, info: UnitInfo | undefined) => ReactNode;
  renderSummaryRows: (item: T) => ReactNode;
}) {
  const { data: info } = useSuspenseQuery({
    ...linuxio.systemd.get_unit_info({ unitName: item.name }),
    refetchInterval: 2000,
  });

  return (
    <UnitCard
      isSelected={true}
      item={item}
      onExpand={onExpand}
      renderActions={renderActions && ((i: T) => renderActions(i, info))}
      renderSelectedRows={
        renderSelectedRows && ((i: T) => renderSelectedRows(i, info))
      }
      renderSummaryRows={renderSummaryRows}
    />
  );
}

function AutoStartRow({ unitFileState }: { unitFileState: string }) {
  const { label, auto } = formatUnitFileState(unitFileState);

  return (
    <DetailRow label="Auto-start">
      <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
        {auto ? (
          <Icon
            color="var(--app-palette-success-main)"
            height={15}
            icon="mdi:check"
            width={15}
          />
        ) : (
          <Icon
            color="var(--app-palette-text-disabled)"
            height={15}
            icon="mdi:block-helper"
            width={15}
          />
        )}
        <AppTypography component="span" fontWeight={500} variant="caption">
          {label}
        </AppTypography>
      </div>
    </DetailRow>
  );
}

interface UnitStatusRowsProps {
  activeEnterTimestamp?: number;
  activeLabel?: string;
  activeState: string;
  inactiveEnterTimestamp?: number;
  subState: string;
  unitFileState: string;
}

export function UnitStatusRows({
  activeState,
  subState,
  unitFileState,
  activeEnterTimestamp,
  inactiveEnterTimestamp,
  activeLabel,
}: UnitStatusRowsProps) {
  const statusColor = getServiceStatusColor(activeState);
  const isActive = activeState === "active";
  const timestamp = formatSystemdUsec(
    isActive ? activeEnterTimestamp : inactiveEnterTimestamp,
    SHORT_DATE_TIME,
  );

  return (
    <>
      <DetailRow label="Status" noBorder>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <AppTypography
            color={statusColor}
            component="span"
            fontWeight={600}
            variant="body2"
          >
            {isActive ? (activeLabel ?? activeState) : activeState}
            {subState && subState !== activeState && (
              <span
                style={{
                  color: "var(--app-palette-text-secondary)",
                  marginLeft: 8,
                  fontWeight: 400,
                }}
              >
                ({subState})
              </span>
            )}
          </AppTypography>
          {/*
            Units without a transition timestamp keep the line as an invisible
            spacer: dropping it outright makes their card a line shorter than
            its neighbours, which is what made the grid look ragged.
          */}
          <AppTypography
            aria-hidden={timestamp === "—" ? true : undefined}
            color="text.secondary"
            component="span"
            style={{
              visibility: timestamp === "—" ? "hidden" : undefined,
            }}
            variant="caption"
          >
            {timestamp === "—"
              ? " "
              : `${isActive ? "Active" : "Inactive"} since ${timestamp}`}
          </AppTypography>
        </div>
      </DetailRow>
      <AutoStartRow unitFileState={unitFileState} />
    </>
  );
}

export const UnitCardActions = ({
  unitName,
  activeState,
  unitFileState,
  info,
}: {
  unitName: string;
  activeState: string;
  unitFileState: string;
  info: UnitInfo | undefined;
}) => {
  const actionConfig = (verb: string) => ({
    success: `${unitName} ${verb}`,
    error: `Failed to ${verb} ${unitName}`,
    toast: SERVICES_TOAST_META,
  });

  const { mutate: startService, isPending: isStarting } = useCallMutation(
    linuxio.systemd.start_service,
    actionConfig("started"),
  );
  const { mutate: stopService, isPending: isStopping } = useCallMutation(
    linuxio.systemd.stop_service,
    actionConfig("stopped"),
  );
  const { mutate: restartService, isPending: isRestarting } = useCallMutation(
    linuxio.systemd.restart_service,
    actionConfig("restarted"),
  );
  const { mutate: reloadService, isPending: isReloading } = useCallMutation(
    linuxio.systemd.reload_service,
    actionConfig("reloaded"),
  );
  const { mutate: enableService, isPending: isEnabling } = useCallMutation(
    linuxio.systemd.enable_service,
    actionConfig("enabled"),
  );
  const { mutate: disableService, isPending: isDisabling } = useCallMutation(
    linuxio.systemd.disable_service,
    actionConfig("disabled"),
  );
  const { mutate: maskService, isPending: isMasking } = useCallMutation(
    linuxio.systemd.mask_service,
    actionConfig("masked"),
  );
  const { mutate: unmaskService, isPending: isUnmasking } = useCallMutation(
    linuxio.systemd.unmask_service,
    actionConfig("unmasked"),
  );
  const { mutate: resetFailedService, isPending: isResettingFailed } =
    useCallMutation(
      linuxio.systemd.reset_failed_service,
      actionConfig("reset"),
    );

  const isActive = activeState === "active";
  const isFailed = activeState === "failed";
  const liveUnitFileState = info?.UnitFileState ?? unitFileState ?? "";
  const isEnabled =
    liveUnitFileState === "enabled" || liveUnitFileState === "enabled-runtime";
  const isMasked = liveUnitFileState === "masked";
  const anyPending =
    isStarting ||
    isStopping ||
    isRestarting ||
    isReloading ||
    isEnabling ||
    isDisabling ||
    isMasking ||
    isUnmasking ||
    isResettingFailed;

  return (
    <div
      onClick={(e) => e.stopPropagation()}
      style={{
        display: "flex",
        flexWrap: "wrap",
        gap: 2,
        marginTop: 12,
        minWidth: 0,
      }}
    >
      {isActive ? (
        <AppActionIconButton
          ariaLabel={`Stop ${unitName}`}
          color="var(--app-palette-error-main)"
          disabled={anyPending}
          icon="mdi:stop-circle-outline"
          iconSize={16}
          label="Stop"
          loading={isStopping}
          onClick={() => stopService({ serviceName: unitName })}
        />
      ) : (
        <AppActionIconButton
          ariaLabel={`Start ${unitName}`}
          color="var(--app-palette-success-main)"
          disabled={anyPending}
          icon="mdi:play"
          iconSize={16}
          label="Start"
          loading={isStarting}
          onClick={() => startService({ serviceName: unitName })}
        />
      )}
      <AppActionIconButton
        ariaLabel={`Restart ${unitName}`}
        disabled={!isActive || anyPending}
        icon="mdi:restart"
        iconSize={16}
        label="Restart (stop then start)"
        loading={isRestarting}
        onClick={() => restartService({ serviceName: unitName })}
      />
      <AppActionIconButton
        ariaLabel={`Reload ${unitName}`}
        disabled={!isActive || anyPending}
        icon="mdi:refresh"
        iconSize={16}
        label="Reload configuration without restarting (if supported)"
        loading={isReloading}
        onClick={() => reloadService({ serviceName: unitName })}
      />
      {isEnabled ? (
        <AppActionIconButton
          ariaLabel={`Disable ${unitName}`}
          disabled={isMasked || anyPending}
          icon="mdi:block-helper"
          iconSize={16}
          label="Disable autostart at boot"
          loading={isDisabling}
          onClick={() => disableService({ serviceName: unitName })}
        />
      ) : (
        <AppActionIconButton
          ariaLabel={`Enable ${unitName}`}
          color="var(--app-palette-success-main)"
          disabled={isMasked || anyPending}
          icon="mdi:play"
          iconSize={16}
          label="Enable autostart at boot"
          loading={isEnabling}
          onClick={() => enableService({ serviceName: unitName })}
        />
      )}
      {isMasked ? (
        <AppActionIconButton
          ariaLabel={`Unmask ${unitName}`}
          color="var(--app-palette-warning-main)"
          disabled={anyPending}
          icon="mdi:eye"
          iconSize={16}
          label="Unmask to allow the unit to be started"
          loading={isUnmasking}
          onClick={() => unmaskService({ serviceName: unitName })}
        />
      ) : (
        <AppActionIconButton
          ariaLabel={`Mask ${unitName}`}
          disabled={anyPending}
          icon="mdi:eye-off"
          iconSize={16}
          label="Mask to completely prevent the unit from starting"
          loading={isMasking}
          onClick={() => maskService({ serviceName: unitName })}
        />
      )}
      {isFailed && (
        <AppActionIconButton
          ariaLabel={`Reset ${unitName}`}
          color="var(--app-palette-warning-main)"
          disabled={anyPending}
          icon="mdi:broom"
          iconSize={16}
          label="Clear the failed state so the unit can be started again"
          loading={isResettingFailed}
          onClick={() => resetFailedService({ serviceName: unitName })}
        />
      )}
    </div>
  );
};

export function UnitTableView<T extends RowData>({
  dnd,
  data,
  desktopColumns,
  mobileColumns,
  getRowKey,
  renderMainRow,
  onSelect,
  emptyMessage,
}: UnitTableViewProps<T>) {
  const isMobile = useMediaQuery(down("sm"));
  const activeColumns = isMobile ? mobileColumns : desktopColumns;
  const columns = useMemo<AppVirtualTableColumnDef<T>[]>(() => {
    const renderedCellCache = new Map<
      string,
      { cells: ReactNode[]; original: T; rowIndex: number }
    >();

    return activeColumns.map((column, columnIndex) => ({
      id: column.field,
      header: column.headerName,
      cell: ({ row }) => {
        const rowKey = String(getRowKey(row.original, row.index));
        let cached = renderedCellCache.get(rowKey);
        if (
          !cached ||
          cached.original !== row.original ||
          cached.rowIndex !== row.index
        ) {
          cached = {
            cells: renderMainRow(row.original, isMobile, row.index),
            original: row.original,
            rowIndex: row.index,
          };
          renderedCellCache.set(rowKey, cached);
        }
        return cached.cells[columnIndex];
      },
      meta: {
        align: column.align,
        cellStyle: column.style,
        headerStyle: column.style,
        width: column.width,
      },
    }));
  }, [activeColumns, getRowKey, isMobile, renderMainRow]);
  const getTableRowId = useCallback(
    (row: T, index: number) => String(getRowKey(row, index)),
    [getRowKey],
  );
  // Selecting a unit swaps the whole list for its detail layout, so the table
  // never renders a selected row — there is nothing to toggle back off.
  const handleRowClick = useCallback(
    ({ original, index }: { original: T; index: number }) => {
      onSelect?.(getRowKey(original, index));
    },
    [getRowKey, onSelect],
  );
  return (
    <AppVirtualTable
      ariaLabel="Units"
      columns={columns}
      data={data}
      dnd={dnd}
      emptyMessage={emptyMessage}
      fillAvailable
      getRowId={getTableRowId}
      onRowClick={handleRowClick}
    />
  );
}

export function UnitCardsView<T extends UnitListItem>({
  surface,
  items,
  expanded,
  onExpand,
  renderSummaryRows,
  renderSelectedRows,
  renderActions,
  renderDetailPanel,
  renderBottomPanel,
  emptyMessage,
}: UnitCardsViewProps<T>) {
  const expandedItem = items.find((item) => item.name === expanded) ?? null;
  // Fed to ReorderableCardGrid's `virtualized` path, which hands it straight
  // to AppVirtualGrid's hand-memoized row — that memo only holds if this
  // callback keeps its identity across renders. Captures are surface.editMode
  // (a primitive) and the onExpand/renderSummaryRows props, all stable.
  const renderUnitCard = useCallback(
    (item: T) => (
      <UnitCard
        isSelected={false}
        item={item}
        onExpand={surface.editMode ? noopExpand : onExpand}
        renderSummaryRows={renderSummaryRows}
      />
    ),
    [surface.editMode, onExpand, renderSummaryRows],
  );

  if (!expandedItem) {
    return (
      <ReorderableCardGrid
        ariaLabel="Units"
        emptyMessage={emptyMessage}
        estimateItemHeight={UNIT_CARD_ESTIMATE_HEIGHT}
        fillAvailable
        getId={getUnitId}
        items={items}
        minItemWidth={UNIT_CARD_MIN_WIDTH}
        renderItem={renderUnitCard}
        surface={surface}
        virtualized
      />
    );
  }

  return (
    <ResourceDetailsLayout
      below={renderBottomPanel?.(expandedItem)}
      onClose={() => onExpand(null)}
      resourceLabel="unit"
      subtitle={expandedItem.name}
      summary={
        <ExpandedUnitCard
          item={expandedItem}
          onExpand={onExpand}
          renderActions={renderActions}
          renderSelectedRows={renderSelectedRows}
          renderSummaryRows={renderSummaryRows}
        />
      }
      summaryWidth="content"
      title="Unit file & dependencies"
    >
      {renderDetailPanel(expandedItem)}
    </ResourceDetailsLayout>
  );
}
