import { Icon } from "@iconify/react";
import { motion } from "framer-motion";
import React from "react";

import { linuxio } from "@/api";
import type { UnitInfo } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppButton from "@/components/ui/AppButton";
import AppGrid from "@/components/ui/AppGrid";
import AppTooltip from "@/components/ui/AppTooltip";
import StatusDot from "@/components/ui/StatusDot";
import { getServiceStatusColor } from "@/constants/statusColors";
import { useAppTheme, useAppMediaQuery } from "@/theme";

export interface UnitListItem {
  name: string;
  description?: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  unit_file_state: string;
}

export interface UnitInfoRow {
  label: string;
  value: React.ReactNode;
  hidden?: boolean;
  noBorder?: boolean;
}

interface UnitTableViewProps<T> {
  data: T[];
  desktopColumns: UnifiedTableColumn[];
  mobileColumns: UnifiedTableColumn[];
  getRowKey: (row: T, index: number) => string | number;
  renderMainRow: (row: T, isMobile: boolean, index: number) => React.ReactNode;
  renderMobileExpandedContent?: (row: T, index: number) => React.ReactNode;
  selected?: string | number | null;
  onSelect?: (key: string | number | null) => void;
  onDoubleClick?: (key: string | number) => void;
  emptyMessage: string;
}

interface UnitCardsViewProps<T extends UnitListItem> {
  items: T[];
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderSummaryRows: (item: T) => React.ReactNode;
  renderSelectedRows?: (item: T) => React.ReactNode;
  renderActions?: (item: T) => React.ReactNode;
  renderDetailPanel: (item: T) => React.ReactNode;
  renderBottomPanel?: (item: T) => React.ReactNode;
  emptyMessage: string;
}

interface UnitInfoPanelProps {
  unitName: string;
  onClose: () => void;
  title?: string;
  renderInfoRows?: (
    info: UnitInfo | undefined,
    isPending: boolean,
  ) => UnitInfoRow[];
}

const labelStyle: React.CSSProperties = {
  textTransform: "uppercase",
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "var(--app-palette-text-secondary)",
  flexShrink: 0,
  width: 90,
  paddingTop: 3,
};

const baseCardStyle: React.CSSProperties = {
  padding: 12,
  display: "flex",
  flexDirection: "column",
  height: "100%",
  cursor: "pointer",
  transition:
    "transform 0.2s, box-shadow 0.2s, border 0.3s ease-in-out, margin 0.3s ease-in-out",
  borderBottomWidth: 2,
  borderBottomStyle: "solid",
};

const cardStyle: React.CSSProperties = {
  ...baseCardStyle,
  borderBottomColor:
    "color-mix(in srgb, var(--svc-status-color), transparent 70%)",
};

const selectedCardStyle: React.CSSProperties = {
  ...baseCardStyle,
  width: "100%",
  borderBottomColor: "var(--svc-status-color)",
};

const depFields: Array<{ label: string; key: keyof UnitInfo }> = [
  { label: "Requires", key: "Requires" },
  { label: "Wants", key: "Wants" },
  { label: "Wanted by", key: "WantedBy" },
  { label: "Triggered by", key: "TriggeredBy" },
  { label: "Part of", key: "PartOf" },
  { label: "Conflicts", key: "Conflicts" },
  { label: "Before", key: "Before" },
  { label: "After", key: "After" },
];

export const DetailRow: React.FC<{
  label: string;
  children: React.ReactNode;
  noBorder?: boolean;
}> = ({ label, children, noBorder }) => (
  <div
    className="svc-detail-row"
    style={{
      display: "flex",
      padding: "3px 0",
      borderTop: noBorder ? undefined : "1px solid var(--app-palette-divider)",
      alignItems: "flex-start",
    }}
  >
    <span style={labelStyle}>{label}</span>
    <div style={{ flex: 1, minWidth: 0 }}>{children}</div>
  </div>
);

export function statusDot(activeState: string) {
  return (
    <StatusDot
      color={getServiceStatusColor(activeState)}
      style={{ marginRight: 8 }}
    />
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
            icon="mdi:check"
            width={15}
            height={15}
            color="var(--app-palette-success-main)"
          />
        ) : (
          <Icon
            icon="mdi:block-helper"
            width={15}
            height={15}
            color="var(--app-palette-text-disabled)"
          />
        )}
        <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>{label}</span>
      </div>
    </DetailRow>
  );
}

interface UnitStatusRowsProps {
  activeState: string;
  subState: string;
  unitFileState: string;
  activeEnterTimestamp?: number;
  inactiveEnterTimestamp?: number;
  activeLabel?: string;
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

export const UnitCardActions: React.FC<{
  unitName: string;
  activeState: string;
  unitFileState: string;
  info: UnitInfo | undefined;
}> = ({ unitName, activeState, unitFileState, info }) => {
  const { mutate: startService, isPending: isStarting } =
    linuxio.dbus.start_service.useMutation();
  const { mutate: stopService, isPending: isStopping } =
    linuxio.dbus.stop_service.useMutation();
  const { mutate: restartService, isPending: isRestarting } =
    linuxio.dbus.restart_service.useMutation();
  const { mutate: reloadService, isPending: isReloading } =
    linuxio.dbus.reload_service.useMutation();
  const { mutate: enableService, isPending: isEnabling } =
    linuxio.dbus.enable_service.useMutation();
  const { mutate: disableService, isPending: isDisabling } =
    linuxio.dbus.disable_service.useMutation();
  const { mutate: maskService, isPending: isMasking } =
    linuxio.dbus.mask_service.useMutation();
  const { mutate: unmaskService, isPending: isUnmasking } =
    linuxio.dbus.unmask_service.useMutation();
  const { mutate: resetFailedService, isPending: isResettingFailed } =
    linuxio.dbus.reset_failed_service.useMutation();

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
      style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 12 }}
      onClick={(e) => e.stopPropagation()}
    >
      {isActive ? (
        <AppTooltip title="Stop">
          <AppButton
            size="small"
            variant="outlined"
            color="error"
            startIcon={<Icon icon="mdi:stop-circle" width={20} height={20} />}
            onClick={() => stopService([unitName])}
            disabled={anyPending}
          >
            Stop
          </AppButton>
        </AppTooltip>
      ) : (
        <AppTooltip title="Start">
          <AppButton
            size="small"
            variant="outlined"
            color="success"
            startIcon={<Icon icon="mdi:play" width={20} height={20} />}
            onClick={() => startService([unitName])}
            disabled={anyPending}
          >
            Start
          </AppButton>
        </AppTooltip>
      )}
      <AppTooltip title="Restart (stop then start)">
        <span>
          <AppButton
            size="small"
            variant="outlined"
            startIcon={<Icon icon="mdi:restart" width={20} height={20} />}
            onClick={() => restartService([unitName])}
            disabled={!isActive || anyPending}
          >
            Restart
          </AppButton>
        </span>
      </AppTooltip>
      <AppTooltip title="Reload configuration without restarting (if supported)">
        <span>
          <AppButton
            size="small"
            variant="outlined"
            startIcon={<Icon icon="mdi:refresh" width={20} height={20} />}
            onClick={() => reloadService([unitName])}
            disabled={!isActive || anyPending}
          >
            Reload
          </AppButton>
        </span>
      </AppTooltip>
      {isEnabled ? (
        <AppTooltip title="Disable autostart at boot">
          <span>
            <AppButton
              size="small"
              variant="outlined"
              startIcon={
                <Icon icon="mdi:block-helper" width={20} height={20} />
              }
              onClick={() => disableService([unitName])}
              disabled={isMasked || anyPending}
            >
              Disable
            </AppButton>
          </span>
        </AppTooltip>
      ) : (
        <AppTooltip title="Enable autostart at boot">
          <span>
            <AppButton
              size="small"
              variant="outlined"
              color="success"
              startIcon={<Icon icon="mdi:play" width={20} height={20} />}
              onClick={() => enableService([unitName])}
              disabled={isMasked || anyPending}
            >
              Enable
            </AppButton>
          </span>
        </AppTooltip>
      )}
      {isMasked ? (
        <AppTooltip title="Unmask to allow the unit to be started">
          <AppButton
            size="small"
            variant="outlined"
            color="warning"
            startIcon={<Icon icon="mdi:eye" width={20} height={20} />}
            onClick={() => unmaskService([unitName])}
            disabled={anyPending}
          >
            Unmask
          </AppButton>
        </AppTooltip>
      ) : (
        <AppTooltip title="Mask to completely prevent the unit from starting">
          <AppButton
            size="small"
            variant="outlined"
            startIcon={<Icon icon="mdi:eye-off" width={20} height={20} />}
            onClick={() => maskService([unitName])}
            disabled={anyPending}
          >
            Mask
          </AppButton>
        </AppTooltip>
      )}
      {isFailed && (
        <AppTooltip title="Clear the failed state so the unit can be started again">
          <AppButton
            size="small"
            variant="outlined"
            color="warning"
            startIcon={<Icon icon="mdi:broom" width={20} height={20} />}
            onClick={() => resetFailedService([unitName])}
            disabled={anyPending}
          >
            Reset Failed
          </AppButton>
        </AppTooltip>
      )}
    </div>
  );
};

function toStringArray(val: unknown): string[] {
  if (!Array.isArray(val)) return [];
  return val.filter((v): v is string => typeof v === "string" && v.length > 0);
}

export function UnitInfoPanel({
  unitName,
  onClose,
  title = "Unit file & dependencies",
  renderInfoRows,
}: UnitInfoPanelProps) {
  const { data: info, isPending } = linuxio.dbus.get_unit_info.useQuery(
    unitName,
    {
      refetchInterval: 2000,
    },
  );

  const fragmentPath = String(info?.FragmentPath ?? "");
  const extraRows = renderInfoRows?.(info, isPending) ?? [];

  return (
    <FrostedCard
      style={{
        padding: 12,
        height: "100%",
        flex: 1,
        display: "flex",
        flexDirection: "column",
      }}
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          marginBottom: 12,
          gap: 8,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: "0.875rem",
              fontWeight: "bold",
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {title}
          </div>
        </div>

        <button
          onClick={onClose}
          style={{
            background: "none",
            border: "none",
            cursor: "pointer",
            padding: 4,
            borderRadius: 4,
            color: "var(--app-palette-text-secondary)",
            display: "flex",
            alignItems: "center",
            flexShrink: 0,
          }}
        >
          <Icon icon="mdi:close" width={20} height={20} />
        </button>
      </div>

      <div style={{ flex: 1 }}>
        <DetailRow label="Path" noBorder>
          {isPending ? (
            <div
              style={{
                height: 18,
                width: "80%",
                borderRadius: 4,
                backgroundColor: "var(--app-palette-action-hover)",
              }}
            />
          ) : (
            <span
              style={{
                fontSize: "0.8rem",
                fontWeight: 500,
                wordBreak: "break-all",
              }}
            >
              {fragmentPath || "—"}
            </span>
          )}
        </DetailRow>

        {extraRows
          .filter((row) => !row.hidden)
          .map((row) => (
            <DetailRow
              key={row.label}
              label={row.label}
              noBorder={row.noBorder}
            >
              <span
                style={{
                  fontSize: "0.75rem",
                  fontWeight: 500,
                  wordBreak: "break-word",
                }}
              >
                {row.value}
              </span>
            </DetailRow>
          ))}

        {!isPending &&
          depFields.map(({ label, key }) => {
            const items = toStringArray(info?.[key]);
            if (!items.length) return null;
            return (
              <DetailRow key={label} label={label}>
                <span
                  style={{
                    fontSize: "0.75rem",
                    fontWeight: 500,
                    wordBreak: "break-word",
                  }}
                >
                  {items.join(", ")}
                </span>
              </DetailRow>
            );
          })}
      </div>
    </FrostedCard>
  );
}

export function UnitTableView<T>({
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

  return (
    <UnifiedCollapsibleTable
      data={data}
      columns={isMobile ? mobileColumns : desktopColumns}
      getRowKey={getRowKey}
      selectedKey={selected}
      onRowClick={
        isMobile
          ? undefined
          : (row, index) => {
              const rowKey = getRowKey(row, index);
              onSelect?.(selected === rowKey ? null : rowKey);
            }
      }
      onRowDoubleClick={(row, index) => onDoubleClick?.(getRowKey(row, index))}
      renderExpandedContent={
        isMobile && renderMobileExpandedContent
          ? (row, index) => renderMobileExpandedContent(row, index)
          : undefined
      }
      renderMainRow={(row, index) => renderMainRow(row, isMobile, index)}
      emptyMessage={emptyMessage}
    />
  );
}

function UnitCard<T extends UnitListItem>({
  item,
  isSelected,
  onExpand,
  renderSummaryRows,
  renderSelectedRows,
  renderActions,
}: {
  item: T;
  isSelected: boolean;
  onExpand: (name: string | null) => void;
  renderSummaryRows: (item: T) => React.ReactNode;
  renderSelectedRows?: (item: T) => React.ReactNode;
  renderActions?: (item: T) => React.ReactNode;
}) {
  const statusColor = getServiceStatusColor(item.active_state);

  return (
    <FrostedCard
      onClick={() => onExpand(isSelected ? null : item.name)}
      hoverLift={!isSelected}
      className="fc-svc-card"
      style={
        {
          "--svc-status-color": statusColor,
          ...(isSelected ? selectedCardStyle : cardStyle),
        } as React.CSSProperties
      }
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          marginBottom: 12,
          gap: 8,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: "0.875rem",
              fontWeight: "bold",
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {item.name}
          </div>
          {item.description && (
            <div
              style={{
                marginTop: 2,
                fontSize: "0.7rem",
                color: "var(--app-palette-text-secondary)",
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
              }}
              title={item.description}
            >
              {item.description}
            </div>
          )}
        </div>
        <span
          style={{
            display: "inline-block",
            width: 8,
            height: 8,
            borderRadius: "50%",
            backgroundColor: statusColor,
            flexShrink: 0,
            marginTop: 4,
          }}
        />
      </div>

      <div
        style={{ flex: 1, display: "flex", flexDirection: "column" }}
        className="svc-card-details"
      >
        <div style={{ flex: 1 }} className="svc-rows-wrapper">
          {renderSummaryRows(item)}
          {isSelected && renderSelectedRows?.(item)}
        </div>
        {isSelected && renderActions && (
          <div onClick={(e) => e.stopPropagation()}>{renderActions(item)}</div>
        )}
      </div>
    </FrostedCard>
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
  const isCompactLayout = useAppMediaQuery(theme.breakpoints.down("md"));
  const expandedItem = items.find((item) => item.name === expanded) ?? null;

  if (items.length === 0) {
    return (
      <div style={{ textAlign: "center", padding: "32px 0" }}>
        <span
          style={{
            fontSize: "0.875rem",
            color: "var(--app-palette-text-secondary)",
          }}
        >
          {emptyMessage}
        </span>
      </div>
    );
  }

  if (!expandedItem) {
    return (
      <AppGrid container spacing={3}>
        {items.map((item) => (
          <AppGrid key={item.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
            <UnitCard
              item={item}
              isSelected={false}
              onExpand={onExpand}
              renderSummaryRows={renderSummaryRows}
            />
          </AppGrid>
        ))}
      </AppGrid>
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
      <div
        style={{
          display: "flex",
          flexDirection: isCompactLayout ? "column" : "row",
          alignItems: "stretch",
          gap: theme.spacing(2.5),
        }}
      >
        <div
          style={{
            width: isCompactLayout ? "100%" : "33.33%",
            flexShrink: 0,
            display: "flex",
          }}
        >
          <UnitCard
            item={expandedItem}
            isSelected={true}
            onExpand={onExpand}
            renderSummaryRows={renderSummaryRows}
            renderSelectedRows={renderSelectedRows}
            renderActions={renderActions}
          />
        </div>
        <motion.div
          style={{ flex: 1, display: "flex", width: "100%" }}
          initial={{
            opacity: 0,
            x: isCompactLayout ? 0 : 40,
            y: isCompactLayout ? 20 : 0,
          }}
          animate={{ opacity: 1, x: 0, y: 0 }}
          transition={{ duration: 0.25, delay: 0.05 }}
        >
          {renderDetailPanel(expandedItem)}
        </motion.div>
      </div>
      {renderBottomPanel && (
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25, delay: 0.1 }}
        >
          {renderBottomPanel(expandedItem)}
        </motion.div>
      )}
    </div>
  );
}
