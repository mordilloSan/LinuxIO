import { useSuspenseQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

import type { UnitInfo } from "@/api";
import { linuxio } from "@/api";
import AppTypography from "@/components/ui/AppTypography";
import { DetailRow } from "@/components/ui/InfoRow";

export interface UnitInfoRow {
  hidden?: boolean;
  label: string;
  noBorder?: boolean;
  value: ReactNode;
}

interface UnitInfoPanelProps {
  renderInfoRows?: (info: UnitInfo | undefined) => UnitInfoRow[];
  unitName: string;
  queryOptions?: ReturnType<typeof linuxio.systemd.get_unit_info>;
}

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

/** The unit file path, kind-specific rows and dependencies of one unit. */
const UnitInfoPanel = ({
  unitName,
  renderInfoRows,
  queryOptions,
}: UnitInfoPanelProps) => {
  const { data: info } = useSuspenseQuery({
    ...(queryOptions ?? linuxio.systemd.get_unit_info({ unitName })),
    refetchInterval: 2000,
  });

  const fragmentPath = info?.FragmentPath ?? "";
  const extraRows = renderInfoRows?.(info) ?? [];

  return (
    <div style={{ flex: 1, overflowX: "auto" }}>
      <div style={{ minWidth: "max-content" }}>
        <DetailRow label="Path" noBorder>
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
                  typeof row.value === "string" || typeof row.value === "number"
                    ? String(row.value)
                    : undefined
                }
                variant="body2"
              >
                {row.value}
              </AppTypography>
            </DetailRow>
          ))}

        {depFields.map(({ label, key }) => {
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
  );
};

export default UnitInfoPanel;
