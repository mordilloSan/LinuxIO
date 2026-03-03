import BlockIcon from "@mui/icons-material/Block";
import CheckIcon from "@mui/icons-material/Check";
import CloseIcon from "@mui/icons-material/Close";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import RefreshIcon from "@mui/icons-material/Refresh";
import RestartAltIcon from "@mui/icons-material/RestartAlt";
import StopCircleIcon from "@mui/icons-material/StopCircle";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Box,
  Button,
  Grid,
  Tooltip,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import { type Theme } from "@mui/material/styles";
import { motion } from "framer-motion";
import React from "react";

import { linuxio } from "@/api";
import type { UnitInfo } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import { getServiceStatusColor } from "@/constants/statusColors";
import { getFrostedCardLiftStyles } from "@/theme/surfaces";

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
  color: "var(--mui-palette-text-secondary)",
  flexShrink: 0,
  width: 90,
  paddingTop: 3,
};

const cardSx = (theme: Theme) => ({
  p: 3,
  display: "flex",
  flexDirection: "column",
  height: "100%",
  cursor: "pointer",
  transition:
    "transform 0.2s, box-shadow 0.2s, border 0.3s ease-in-out, margin 0.3s ease-in-out",
  borderBottomWidth: "2px",
  borderBottomStyle: "solid",
  borderBottomColor:
    "color-mix(in srgb, var(--svc-status-color), transparent 70%)",
  "&:hover": {
    ...getFrostedCardLiftStyles(theme),
  },
  "& .svc-rows-wrapper > .svc-detail-row:last-of-type": {
    borderBottom: "none",
  },
});

const selectedCardSx = {
  p: 3,
  display: "flex",
  flexDirection: "column",
  height: "100%",
  width: "100%",
  cursor: "pointer",
  transition:
    "transform 0.2s, box-shadow 0.2s, border 0.3s ease-in-out, margin 0.3s ease-in-out",
  borderBottomWidth: "2px",
  borderBottomStyle: "solid",
  borderBottomColor: "var(--svc-status-color)",
  "& .svc-rows-wrapper > .svc-detail-row:last-of-type": {
    borderBottom: "none",
  },
} as const;

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
      padding: "1px 0",
      borderTop: noBorder ? undefined : "1px solid var(--mui-palette-divider)",
      alignItems: "flex-start",
    }}
  >
    <span style={labelStyle}>{label}</span>
    <div style={{ flex: 1, minWidth: 0 }}>{children}</div>
  </div>
);

export function statusDot(activeState: string) {
  return (
    <Box
      component="span"
      sx={{
        display: "inline-block",
        width: 10,
        height: 10,
        borderRadius: "50%",
        bgcolor: getServiceStatusColor(activeState),
        mr: 1,
        flexShrink: 0,
      }}
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
          <CheckIcon
            style={{
              fontSize: 15,
              color: "var(--mui-palette-success-main)",
            }}
          />
        ) : (
          <BlockIcon
            style={{
              fontSize: 15,
              color: "var(--mui-palette-text-disabled)",
            }}
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
        <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
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
                  color: "var(--mui-palette-text-secondary)",
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
                color: "var(--mui-palette-text-secondary)",
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

  const isActive = activeState === "active";
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
    isUnmasking;

  return (
    <div
      style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 12 }}
      onClick={(e) => e.stopPropagation()}
    >
      {isActive ? (
        <Tooltip title="Stop">
          <Button
            size="small"
            variant="outlined"
            color="error"
            startIcon={<StopCircleIcon fontSize="small" />}
            onClick={() => stopService([unitName])}
            disabled={anyPending}
          >
            Stop
          </Button>
        </Tooltip>
      ) : (
        <Tooltip title="Start">
          <Button
            size="small"
            variant="outlined"
            color="success"
            startIcon={<PlayArrowIcon fontSize="small" />}
            onClick={() => startService([unitName])}
            disabled={anyPending}
          >
            Start
          </Button>
        </Tooltip>
      )}
      <Tooltip title="Restart (stop then start)">
        <span>
          <Button
            size="small"
            variant="outlined"
            startIcon={<RestartAltIcon fontSize="small" />}
            onClick={() => restartService([unitName])}
            disabled={!isActive || anyPending}
          >
            Restart
          </Button>
        </span>
      </Tooltip>
      <Tooltip title="Reload configuration without restarting (if supported)">
        <span>
          <Button
            size="small"
            variant="outlined"
            startIcon={<RefreshIcon fontSize="small" />}
            onClick={() => reloadService([unitName])}
            disabled={!isActive || anyPending}
          >
            Reload
          </Button>
        </span>
      </Tooltip>
      {isEnabled ? (
        <Tooltip title="Disable autostart at boot">
          <span>
            <Button
              size="small"
              variant="outlined"
              startIcon={<BlockIcon fontSize="small" />}
              onClick={() => disableService([unitName])}
              disabled={isMasked || anyPending}
            >
              Disable
            </Button>
          </span>
        </Tooltip>
      ) : (
        <Tooltip title="Enable autostart at boot">
          <span>
            <Button
              size="small"
              variant="outlined"
              color="success"
              startIcon={<PlayArrowIcon fontSize="small" />}
              onClick={() => enableService([unitName])}
              disabled={isMasked || anyPending}
            >
              Enable
            </Button>
          </span>
        </Tooltip>
      )}
      {isMasked ? (
        <Tooltip title="Unmask to allow the unit to be started">
          <Button
            size="small"
            variant="outlined"
            color="warning"
            startIcon={<VisibilityIcon fontSize="small" />}
            onClick={() => unmaskService([unitName])}
            disabled={anyPending}
          >
            Unmask
          </Button>
        </Tooltip>
      ) : (
        <Tooltip title="Mask to completely prevent the unit from starting">
          <Button
            size="small"
            variant="outlined"
            startIcon={<VisibilityOffIcon fontSize="small" />}
            onClick={() => maskService([unitName])}
            disabled={anyPending}
          >
            Mask
          </Button>
        </Tooltip>
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
      sx={{ p: 3, height: "100%", flex: 1, display: "flex", flexDirection: "column" }}
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
            color: "var(--mui-palette-text-secondary)",
            display: "flex",
            alignItems: "center",
            flexShrink: 0,
          }}
        >
          <CloseIcon fontSize="small" />
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
                backgroundColor: "var(--mui-palette-action-hover)",
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
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));

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
      style={{ "--svc-status-color": statusColor } as React.CSSProperties}
      sx={isSelected ? selectedCardSx : cardSx}
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
                fontSize: "0.7rem",
                color: "var(--mui-palette-text-secondary)",
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
          <div onClick={(e) => e.stopPropagation()}>
            {renderActions(item)}
          </div>
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
  const expandedItem = items.find((item) => item.name === expanded) ?? null;

  if (items.length === 0) {
    return (
      <div style={{ textAlign: "center", padding: "32px 0" }}>
        <span
          style={{
            fontSize: "0.875rem",
            color: "var(--mui-palette-text-secondary)",
          }}
        >
          {emptyMessage}
        </span>
      </div>
    );
  }

  if (!expandedItem) {
    return (
      <Grid container spacing={3}>
        {items.map((item) => (
          <Grid key={item.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
            <UnitCard
              item={item}
              isSelected={false}
              onExpand={onExpand}
              renderSummaryRows={renderSummaryRows}
            />
          </Grid>
        ))}
      </Grid>
    );
  }

  return (
    <Box display="flex" flexDirection="column" gap={3}>
      <Box display="flex" alignItems="stretch" gap={2.5}>
        <Box sx={{ width: { xs: "100%", md: "33.33%" }, flexShrink: 0, display: "flex" }}>
          <UnitCard
            item={expandedItem}
            isSelected={true}
            onExpand={onExpand}
            renderSummaryRows={renderSummaryRows}
            renderSelectedRows={renderSelectedRows}
            renderActions={renderActions}
          />
        </Box>
        <motion.div
          style={{ flex: 1, display: "flex" }}
          initial={{ opacity: 0, x: 40 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ duration: 0.25, delay: 0.05 }}
        >
          {renderDetailPanel(expandedItem)}
        </motion.div>
      </Box>
      {renderBottomPanel && (
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25, delay: 0.1 }}
        >
          {renderBottomPanel(expandedItem)}
        </motion.div>
      )}
    </Box>
  );
}
