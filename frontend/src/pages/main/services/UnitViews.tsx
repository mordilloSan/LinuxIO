import BlockIcon from "@mui/icons-material/Block";
import CheckIcon from "@mui/icons-material/Check";
import CloseIcon from "@mui/icons-material/Close";
import { Box, Grid, useMediaQuery, useTheme } from "@mui/material";
import { type Theme } from "@mui/material/styles";
import { AnimatePresence, motion } from "framer-motion";
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
  "& .svc-card-details > .svc-detail-row:last-of-type": {
    borderBottom: "none",
  },
});

const selectedCardSx = {
  p: 3,
  display: "flex",
  flexDirection: "column",
  height: "100%",
  cursor: "pointer",
  transition:
    "transform 0.2s, box-shadow 0.2s, border 0.3s ease-in-out, margin 0.3s ease-in-out",
  borderBottomWidth: "2px",
  borderBottomStyle: "solid",
  borderBottomColor: "var(--svc-status-color)",
  "& .svc-card-details > .svc-detail-row:last-of-type": {
    borderBottom: "none",
  },
} as const;

const expandedPaneSx = {
  display: "flex",
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
      sx={{ p: 3, height: "100%", display: "flex", flexDirection: "column" }}
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
}: {
  item: T;
  isSelected: boolean;
  onExpand: (name: string | null) => void;
  renderSummaryRows: (item: T) => React.ReactNode;
  renderSelectedRows?: (item: T) => React.ReactNode;
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

      <div style={{ flex: 1 }} className="svc-card-details">
        {renderSummaryRows(item)}
        {isSelected && renderSelectedRows?.(item)}
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

  return (
    <Grid container spacing={3}>
      {items.map((item) =>
        expanded && expanded !== item.name ? null : (
          <Grid
            key={item.name}
            size={
              expanded === item.name
                ? { xs: 12, md: 4, lg: 4 }
                : { xs: 12, sm: 6, md: 4, lg: 3 }
            }
            sx={expanded === item.name ? expandedPaneSx : undefined}
          >
            <UnitCard
              item={item}
              isSelected={expanded === item.name}
              onExpand={onExpand}
              renderSummaryRows={renderSummaryRows}
              renderSelectedRows={renderSelectedRows}
            />
          </Grid>
        ),
      )}

      <AnimatePresence initial={false}>
        {expandedItem && (
          <Grid
            key="detail-panel"
            size={{ xs: 12, md: 8, lg: 8 }}
            component={motion.div}
            sx={expandedPaneSx}
            initial={{ opacity: 0, x: 40 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: 40 }}
            transition={{ duration: 0.25, delay: 0.05 }}
          >
            {renderDetailPanel(expandedItem)}
          </Grid>
        )}

        {expandedItem && renderBottomPanel && (
          <Grid
            key="bottom-panel"
            size={{ xs: 12 }}
            component={motion.div}
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 20 }}
            transition={{ duration: 0.25, delay: 0.1 }}
          >
            {renderBottomPanel(expandedItem)}
          </Grid>
        )}
      </AnimatePresence>
    </Grid>
  );
}
