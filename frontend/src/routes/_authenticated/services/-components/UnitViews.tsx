import { Icon } from "@iconify/react";
import type { RowData } from "@tanstack/react-table";
import { motion } from "motion/react";
import {
  Children,
  Fragment,
  isValidElement,
  type CSSProperties,
  type ReactNode,
} from "react";

import type { UnitInfo } from "@/api";
import { linuxio } from "@/api";
import type { UnitListItem } from "@/components/cards/UnitCard";
import UnitCard from "@/components/cards/UnitCard";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import AppVirtualGrid from "@/components/grid/AppVirtualGrid";
import AppVirtualDataTable from "@/components/tables/AppVirtualDataTable";
import type {
  AppVirtualDataTableBreakpoint,
  AppVirtualDataTableColumnDef,
} from "@/components/tables/AppVirtualDataTable";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import StatusDot from "@/components/ui/StatusDot";
import { getServiceStatusColor } from "@/constants/statusColors";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import {
  TRANSITION_DURATION_SLOW_MS,
  EASING_STANDARD,
} from "@/theme/constants";

export type { UnitListItem } from "@/components/cards/UnitCard";
export { DetailRow } from "@/components/cards/UnitInfoPanelCard";
export type { UnitInfoRow } from "@/components/cards/UnitInfoPanelCard";
export { UnitInfoPanel } from "@/components/cards/UnitInfoPanelCard";

const UNIT_CARD_GRID_GAP = 12;
const UNIT_CARD_MIN_WIDTH = 360;
const UNIT_CARD_ESTIMATE_HEIGHT = 150;
const SERVICES_TOAST_META = {
  label: "Open services",
  to: "/services",
} as const;

interface UnitTableViewProps<T> {
  data: T[];
  desktopColumns: UnitTableColumn[];
  emptyMessage: string;
  getRowKey: (row: T, index: number) => string | number;
  mobileColumns: UnitTableColumn[];
  onDoubleClick?: (key: string | number) => void;
  onSelect?: (key: string | number | null) => void;
  renderMainRow: (row: T, isMobile: boolean, index: number) => ReactNode;
  renderMobileExpandedContent?: (row: T, index: number) => ReactNode;
  selected?: string | number | null;
}

interface UnitTableColumn {
  align?: "left" | "center" | "right";
  className?: string;
  field: string;
  headerName: string;
  style?: CSSProperties;
  width?: string | number;
}

interface UnitCardsViewProps<T extends UnitListItem> {
  emptyMessage: string;
  expanded: string | null;
  items: T[];
  onExpand: (name: string | null) => void;
  renderActions?: (item: T) => ReactNode;
  renderBottomPanel?: (item: T) => ReactNode;
  renderDetailPanel: (item: T) => ReactNode;
  renderSelectedRows?: (item: T) => ReactNode;
  renderSummaryRows: (item: T) => ReactNode;
}

export function statusDot(activeState: string) {
  return (
    <StatusDot
      color={getServiceStatusColor(activeState)}
      style={{ marginRight: 8 }}
    />
  );
}

interface MobileExpandedDetail {
  label: string;
  value: ReactNode;
}

export function MobileExpandedDetails({
  rows,
}: {
  rows: readonly MobileExpandedDetail[];
}) {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 6,
        padding: "2px 0",
      }}
    >
      {rows.map(({ label, value }) => (
        <div key={label} style={{ display: "flex", gap: 12 }}>
          <span
            style={{
              fontSize: "0.6rem",
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              color: "var(--app-palette-text-secondary)",
              width: 80,
              flexShrink: 0,
              paddingTop: 2,
            }}
          >
            {label}
          </span>
          <span style={{ fontSize: "0.8rem", fontWeight: 500 }}>{value}</span>
        </div>
      ))}
    </div>
  );
}

export function formatBytes(val: unknown): string {
  const b = Number(val ?? 0);
  if (!b || b > 1e18) return "—";
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} kB`;
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`;
  return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

export function formatTimestamp(ts: unknown): string {
  const ms = Number(ts ?? 0) / 1000;
  if (!ms) return "—";
  return new Date(ms).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatUsec(usec: unknown): string {
  const n = Number(usec ?? 0);
  if (!n || !Number.isFinite(n) || n >= Number.MAX_SAFE_INTEGER) return "—";
  const d = new Date(n / 1000);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString();
}

export function formatUnitFileState(state: string): {
  label: string;
  auto: boolean;
} {
  switch (state) {
    case "enabled":
      return { label: "Automatically starts", auto: true };
    case "enabled-runtime":
      return { label: "Automatically starts (runtime)", auto: true };
    case "static":
      return { label: "Statically enabled", auto: false };
    case "disabled":
      return { label: "Does not automatically start", auto: false };
    case "masked":
      return { label: "Masked (disabled)", auto: false };
    case "generated":
      return { label: "Generated by a generator", auto: false };
    default:
      return { label: state || "—", auto: false };
  }
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
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>{label}</span>
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
          <span
            style={{
              fontSize: "0.85rem",
              fontWeight: 600,
              color: statusColor,
            }}
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
          </span>
          {timestamp !== "—" && (
            <span
              style={{
                fontSize: "0.7rem",
                color: "var(--app-palette-text-secondary)",
              }}
            >
              {isActive ? "Active" : "Inactive"} since {timestamp}
            </span>
          )}
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

  const { mutate: startService, isPending: isStarting } =
    linuxio.systemd.start_service.useAction(actionConfig("started"));
  const { mutate: stopService, isPending: isStopping } =
    linuxio.systemd.stop_service.useAction(actionConfig("stopped"));
  const { mutate: restartService, isPending: isRestarting } =
    linuxio.systemd.restart_service.useAction(actionConfig("restarted"));
  const { mutate: reloadService, isPending: isReloading } =
    linuxio.systemd.reload_service.useAction(actionConfig("reloaded"));
  const { mutate: enableService, isPending: isEnabling } =
    linuxio.systemd.enable_service.useAction(actionConfig("enabled"));
  const { mutate: disableService, isPending: isDisabling } =
    linuxio.systemd.disable_service.useAction(actionConfig("disabled"));
  const { mutate: maskService, isPending: isMasking } =
    linuxio.systemd.mask_service.useAction(actionConfig("masked"));
  const { mutate: unmaskService, isPending: isUnmasking } =
    linuxio.systemd.unmask_service.useAction(actionConfig("unmasked"));
  const { mutate: resetFailedService, isPending: isResettingFailed } =
    linuxio.systemd.reset_failed_service.useAction(actionConfig("reset"));

  const isActive = activeState === "active";
  const isFailed = activeState === "failed";
  const liveUnitFileState = String(info?.UnitFileState ?? unitFileState ?? "");
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

type RenderedTableCellProps = {
  children?: ReactNode;
};

function getHideBelow(
  className?: string,
): AppVirtualDataTableBreakpoint | undefined {
  if (!className) return undefined;
  if (className.includes("app-table-hide-below-xl")) return "xl";
  if (className.includes("app-table-hide-below-lg")) return "lg";
  if (className.includes("app-table-hide-below-md")) return "md";
  if (className.includes("app-table-hide-below-sm")) return "sm";
  return undefined;
}

function flattenRenderedCells(node: ReactNode): ReactNode[] {
  const cells: ReactNode[] = [];

  Children.forEach(node, (child) => {
    if (
      isValidElement<RenderedTableCellProps>(child) &&
      child.type === Fragment
    ) {
      cells.push(...flattenRenderedCells(child.props.children));
      return;
    }

    cells.push(child);
  });

  return cells;
}

function getRenderedCellContent(cell: ReactNode) {
  if (isValidElement<RenderedTableCellProps>(cell)) {
    return cell.props.children;
  }
  return cell;
}

export function UnitTableView<T extends RowData>({
  data,
  desktopColumns,
  mobileColumns,
  getRowKey,
  renderMainRow,
  renderMobileExpandedContent,
  selected,
  onSelect,
  onDoubleClick,
  emptyMessage,
}: UnitTableViewProps<T>) {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const activeColumns = isMobile ? mobileColumns : desktopColumns;
  const renderedCellCache = new Map<string, ReactNode[]>();
  const columns: AppVirtualDataTableColumnDef<T>[] = activeColumns.map(
    (column, columnIndex) => ({
      id: column.field,
      header: column.headerName,
      cell: ({ row }) => {
        const rowKey = String(getRowKey(row.original, row.index));
        let cells = renderedCellCache.get(rowKey);
        if (!cells) {
          cells = flattenRenderedCells(
            renderMainRow(row.original, isMobile, row.index),
          );
          renderedCellCache.set(rowKey, cells);
        }
        return getRenderedCellContent(cells[columnIndex]);
      },
      meta: {
        align: column.align,
        cellStyle: column.style,
        className: column.className,
        headerStyle: column.style,
        hideBelow: getHideBelow(column.className),
        width: column.width,
      },
    }),
  );

  return (
    <AppVirtualDataTable
      ariaLabel="Units"
      columns={columns}
      data={data}
      emptyMessage={emptyMessage}
      fillAvailable
      getRowId={(row, index) => String(getRowKey(row, index))}
      onRowClick={
        isMobile
          ? undefined
          : ({ original, index }) => {
              const rowKey = getRowKey(original, index);
              onSelect?.(selected === rowKey ? null : rowKey);
            }
      }
      onRowDoubleClick={({ original, index }) =>
        onDoubleClick?.(getRowKey(original, index))
      }
      renderExpandedContent={
        isMobile && renderMobileExpandedContent
          ? ({ original, index }) =>
              renderMobileExpandedContent(original, index)
          : undefined
      }
      selectedRowId={
        selected === undefined || selected === null ? null : String(selected)
      }
    />
  );
}

export function UnitCardsView<T extends UnitListItem>({
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
  const theme = useAppTheme();
  const slowTransitionDurationSeconds = TRANSITION_DURATION_SLOW_MS / 1000;
  const isCompactLayout = useAppMediaQuery(theme.breakpoints.down("md"));
  const expandedItem = items.find((item) => item.name === expanded) ?? null;

  if (!expandedItem) {
    return (
      <AppVirtualGrid
        ariaLabel="Units"
        emptyMessage={emptyMessage}
        estimateItemHeight={UNIT_CARD_ESTIMATE_HEIGHT}
        fillAvailable
        gap={UNIT_CARD_GRID_GAP}
        getItemKey={(item) => item.name}
        items={items}
        minItemWidth={UNIT_CARD_MIN_WIDTH}
        padding={0}
        renderItem={(item) => (
          <UnitCard
            isSelected={false}
            item={item}
            onExpand={onExpand}
            renderSummaryRows={renderSummaryRows}
          />
        )}
      />
    );
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(3),
      }}
    >
      <motion.div
        animate={{ opacity: 1, y: 0 }}
        initial={{ opacity: 0, y: 14 }}
        style={{
          display: "flex",
          flexDirection: isCompactLayout ? "column" : "row",
          alignItems: "stretch",
          gap: theme.spacing(2.5),
        }}
        transition={{
          duration: slowTransitionDurationSeconds,
          delay: 0.04,
          ease: EASING_STANDARD,
        }}
      >
        <div
          style={{
            flex: isCompactLayout ? "0 0 auto" : 1,
            width: isCompactLayout ? "100%" : undefined,
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
            width: isCompactLayout ? "100%" : "33.33%",
            flexShrink: 0,
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
