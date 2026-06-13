import { Icon } from "@iconify/react";
import React from "react";

import type { UnitInfo } from "@/api";
import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";

export interface UnitInfoRow {
  hidden?: boolean;
  label: string;
  noBorder?: boolean;
  value: React.ReactNode;
}

interface UnitInfoPanelProps {
  onClose: () => void;
  renderInfoRows?: (
    info: UnitInfo | undefined,
    isPending: boolean,
  ) => UnitInfoRow[];
  title?: string;
  unitName: string;
}

const labelStyle: React.CSSProperties = {
  textTransform: "uppercase",
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "var(--app-palette-text-secondary)",
  flexShrink: 0,
  width: 90,
};

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
      alignItems: "baseline",
    }}
  >
    <span style={labelStyle}>{label}</span>
    <div style={{ flex: 1, minWidth: 0 }}>{children}</div>
  </div>
);

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
  const { data: info, isPending } = linuxio.systemd.get_unit_info.useQuery(
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
          <AppTypography
            component="div"
            fontSize="0.875rem"
            fontWeight="bold"
            noWrap
            title={title || unitName}
            variant="body2"
          >
            {title}
          </AppTypography>
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
          <Icon height={20} icon="mdi:close" width={20} />
        </button>
      </div>

      <div className="custom-scrollbar" style={{ flex: 1, overflowX: "auto" }}>
        <div style={{ minWidth: "max-content" }}>
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
              <AppTypography
                component="span"
                fontSize="0.8rem"
                fontWeight={500}
                noWrap
                title={fragmentPath || "—"}
                variant="body2"
              >
                {fragmentPath || "—"}
              </AppTypography>
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
                <AppTypography
                  component="span"
                  fontSize="0.75rem"
                  fontWeight={500}
                  noWrap
                  title={
                    typeof row.value === "string" ||
                    typeof row.value === "number"
                      ? String(row.value)
                      : undefined
                  }
                  variant="body2"
                >
                  {row.value}
                </AppTypography>
              </DetailRow>
            ))}

          {!isPending &&
            depFields.map(({ label, key }) => {
              const items = toStringArray(info?.[key]);
              if (!items.length) return null;
              return (
                <DetailRow key={label} label={label}>
                  <AppTypography
                    component="span"
                    fontSize="0.75rem"
                    fontWeight={500}
                    noWrap
                    title={items.join(", ")}
                    variant="body2"
                  >
                    {items.join(", ")}
                  </AppTypography>
                </DetailRow>
              );
            })}
        </div>
      </div>
    </FrostedCard>
  );
}

export default UnitInfoPanel;
