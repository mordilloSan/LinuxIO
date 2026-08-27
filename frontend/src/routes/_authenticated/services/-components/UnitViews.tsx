import { Icon } from "@iconify/react";
import type { RowData } from "@tanstack/react-table";
import { motion } from "motion/react";
import {
  useCallback,
  useMemo,
  type CSSProperties,
  type ReactNode,
} from "react";

import type { UnitInfo } from "@/api";
import { linuxio, useCallMutation } from "@/api";
import type { UnitListItem } from "@/components/cards/UnitCard";
import UnitCard from "@/components/cards/UnitCard";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableDndOptions } from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { getServiceStatusColor } from "@/constants/statusColors";
import type { ReorderableSurface } from "@/hooks/useReorderableSurface";
import { useAppMediaQuery } from "@/theme";
import { down } from "@/theme/breakpoints";
import {
  DETAIL_PANEL_GAP,
  TRANSITION_DURATION_SLOW_MS,
  EASING_STANDARD,
} from "@/theme/constants";

import { formatTimestamp, formatUnitFileState } from "./unitFormatters";

export type { UnitListItem } from "@/components/cards/UnitCard";
export { DetailRow } from "@/components/cards/UnitInfoPanelCard";
export type { UnitInfoRow } from "@/components/cards/UnitInfoPanelCard";
export { UnitInfoPanel } from "@/components/cards/UnitInfoPanelCard";

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

interface UnitTableColumn {
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
  renderActions?: (item: T) => ReactNode;
  renderBottomPanel?: (item: T) => ReactNode;
  renderDetailPanel: (item: T) => ReactNode;
  renderSelectedRows?: (item: T) => ReactNode;
  renderSummaryRows: (item: T) => ReactNode;
}

export function AutoStartRow({ unitFileState }: { unitFileState: string }) {
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
  const timestamp = formatTimestamp(
    isActive ? activeEnterTimestamp : inactiveEnterTimestamp,
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

const buttonIcon = (pending: boolean, iconName: string) =>
  pending ? (
    <AppCircularProgress color="inherit" size={16} />
  ) : (
    <Icon height={16} icon={iconName} width={16} />
  );

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
        gap: 6,
        flexWrap: "nowrap",
        marginTop: 12,
        minWidth: 0,
      }}
    >
      {isActive ? (
        <AppTooltip title="Stop">
          <AppButton
            color="error"
            disabled={anyPending}
            onClick={() => stopService({ serviceName: unitName })}
            size="small"
            startIcon={buttonIcon(isStopping, "mdi:stop-circle")}
            variant="outlined"
          >
            Stop
          </AppButton>
        </AppTooltip>
      ) : (
        <AppTooltip title="Start">
          <AppButton
            color="success"
            disabled={anyPending}
            onClick={() => startService({ serviceName: unitName })}
            size="small"
            startIcon={buttonIcon(isStarting, "mdi:play")}
            variant="outlined"
          >
            Start
          </AppButton>
        </AppTooltip>
      )}
      <AppTooltip title="Restart (stop then start)">
        <span>
          <AppButton
            disabled={!isActive || anyPending}
            onClick={() => restartService({ serviceName: unitName })}
            size="small"
            startIcon={buttonIcon(isRestarting, "mdi:restart")}
            variant="outlined"
          >
            Restart
          </AppButton>
        </span>
      </AppTooltip>
      <AppTooltip title="Reload configuration without restarting (if supported)">
        <span>
          <AppButton
            disabled={!isActive || anyPending}
            onClick={() => reloadService({ serviceName: unitName })}
            size="small"
            startIcon={buttonIcon(isReloading, "mdi:refresh")}
            variant="outlined"
          >
            Reload
          </AppButton>
        </span>
      </AppTooltip>
      {isEnabled ? (
        <AppTooltip title="Disable autostart at boot">
          <span>
            <AppButton
              disabled={isMasked || anyPending}
              onClick={() => disableService({ serviceName: unitName })}
              size="small"
              startIcon={buttonIcon(isDisabling, "mdi:block-helper")}
              variant="outlined"
            >
              Disable
            </AppButton>
          </span>
        </AppTooltip>
      ) : (
        <AppTooltip title="Enable autostart at boot">
          <span>
            <AppButton
              color="success"
              disabled={isMasked || anyPending}
              onClick={() => enableService({ serviceName: unitName })}
              size="small"
              startIcon={buttonIcon(isEnabling, "mdi:play")}
              variant="outlined"
            >
              Enable
            </AppButton>
          </span>
        </AppTooltip>
      )}
      {isMasked ? (
        <AppTooltip title="Unmask to allow the unit to be started">
          <AppButton
            color="warning"
            disabled={anyPending}
            onClick={() => unmaskService({ serviceName: unitName })}
            size="small"
            startIcon={buttonIcon(isUnmasking, "mdi:eye")}
            variant="outlined"
          >
            Unmask
          </AppButton>
        </AppTooltip>
      ) : (
        <AppTooltip title="Mask to completely prevent the unit from starting">
          <AppButton
            disabled={anyPending}
            onClick={() => maskService({ serviceName: unitName })}
            size="small"
            startIcon={buttonIcon(isMasking, "mdi:eye-off")}
            variant="outlined"
          >
            Mask
          </AppButton>
        </AppTooltip>
      )}
      {isFailed && (
        <AppTooltip title="Clear the failed state so the unit can be started again">
          <AppButton
            color="warning"
            disabled={anyPending}
            onClick={() => resetFailedService({ serviceName: unitName })}
            size="small"
            startIcon={buttonIcon(isResettingFailed, "mdi:broom")}
            variant="outlined"
          >
            Reset
          </AppButton>
        </AppTooltip>
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
  const isMobile = useAppMediaQuery(down("sm"));
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
  const slowTransitionDurationSeconds = TRANSITION_DURATION_SLOW_MS / 1000;
  const isCompactLayout = useAppMediaQuery(down("md"));
  const expandedItem = items.find((item) => item.name === expanded) ?? null;

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
        renderItem={(item) => (
          <UnitCard
            isSelected={false}
            item={item}
            onExpand={surface.editMode ? noopExpand : onExpand}
            renderSummaryRows={renderSummaryRows}
          />
        )}
        surface={surface}
        virtualized
      />
    );
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-12)",
      }}
    >
      <motion.div
        animate={{ opacity: 1, y: 0 }}
        initial={{ opacity: 0, y: 14 }}
        style={{
          display: "flex",
          flexDirection: isCompactLayout ? "column" : "row",
          alignItems: "stretch",
          gap: DETAIL_PANEL_GAP,
        }}
        transition={{
          duration: slowTransitionDurationSeconds,
          delay: 0.04,
          ease: EASING_STANDARD,
        }}
      >
        <div
          style={{
            flex: isCompactLayout ? "0 0 auto" : "0 0 max-content",
            width: isCompactLayout ? "100%" : "max-content",
            minWidth: 0,
            display: "flex",
          }}
        >
          <UnitCard
            isSelected={true}
            item={expandedItem}
            onExpand={onExpand}
            renderActions={renderActions}
            renderSelectedRows={renderSelectedRows}
            renderSummaryRows={renderSummaryRows}
          />
        </div>
        <motion.div
          animate={{ opacity: 1, x: 0, y: 0 }}
          initial={{
            opacity: 0,
            x: isCompactLayout ? 0 : 40,
            y: isCompactLayout ? 20 : 0,
          }}
          style={{
            flex: isCompactLayout ? "0 0 auto" : 3,
            width: isCompactLayout ? "100%" : undefined,
            minWidth: 0,
            display: "flex",
          }}
          transition={{
            duration: slowTransitionDurationSeconds,
            delay: 0.08,
            ease: EASING_STANDARD,
          }}
        >
          {renderDetailPanel(expandedItem)}
        </motion.div>
      </motion.div>
      {renderBottomPanel && (
        <motion.div
          animate={{ opacity: 1, y: 0 }}
          initial={{ opacity: 0, y: 20 }}
          transition={{
            duration: slowTransitionDurationSeconds,
            delay: 0.16,
            ease: EASING_STANDARD,
          }}
        >
          {renderBottomPanel(expandedItem)}
        </motion.div>
      )}
    </div>
  );
}
