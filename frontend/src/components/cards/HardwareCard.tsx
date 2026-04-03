import { Icon } from "@iconify/react";
import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

export type SummaryRow = {
  label: string;
  value: React.ReactNode;
  noWrap?: boolean;
};

export const SummaryRowsList: React.FC<{ rows: SummaryRow[] }> = ({ rows }) => {
  const theme = useAppTheme();

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignSelf: "stretch",
        width: "100%",
      }}
    >
      {rows.map(({ label, value, noWrap }, index) => (
        <div
          key={label}
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "space-between",
            gap: theme.spacing(1),
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === rows.length - 1
                ? "none"
                : "1px solid var(--app-palette-divider)",
          }}
        >
          <AppTypography
            variant="caption"
            color="text.secondary"
            style={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
          >
            {label}
          </AppTypography>
          <div
            style={{
              minWidth: 0,
              flex: 1,
              display: "flex",
              justifyContent: "flex-end",
              alignItems: "center",
            }}
          >
            {typeof value === "string" ? (
              <AppTypography
                variant="body2"
                fontWeight={500}
                noWrap={noWrap ?? true}
                align="right"
                style={{ width: "100%", textAlign: "right" }}
              >
                {value}
              </AppTypography>
            ) : (
              value
            )}
          </div>
        </div>
      ))}
    </div>
  );
};

const HardwareCard: React.FC<{
  title: string;
  subtitle: string;
  avatarIcon: string;
  accentColor: string;
  rows: SummaryRow[];
  actions?: React.ReactNode;
}> = ({ title, subtitle, avatarIcon, accentColor, rows, actions }) => (
  <FrostedCard
    style={{
      display: "flex",
      flexDirection: "column",
      padding: 8,
    }}
    hoverLift
  >
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 6,
        marginBottom: 6,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 3 }}>
        <div
          style={{
            width: 40,
            height: 40,
            borderRadius: 8,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flexShrink: 0,
          }}
        >
          <Icon icon={avatarIcon} width={28} height={28} color={accentColor} />
        </div>
        <div>
          <AppTypography
            variant="subtitle1"
            fontWeight={700}
            style={{ lineHeight: 1 }}
          >
            {title}
          </AppTypography>
          <AppTypography variant="caption" color="text.secondary">
            {subtitle}
          </AppTypography>
        </div>
      </div>
      {actions && (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 12,
            flexShrink: 0,
          }}
        >
          {actions}
        </div>
      )}
    </div>

    <div style={{ display: "flex", flexWrap: "wrap", gap: 16, flex: 1 }}>
      <div
        style={{
          flex: "1 1 200px",
          minWidth: 0,
          display: "flex",
          alignItems: "stretch",
        }}
      >
        <SummaryRowsList rows={rows} />
      </div>
      <div
        style={{
          flex: "1 1 200px",
          minWidth: 0,
          display: "flex",
          alignItems: "stretch",
        }}
      />
    </div>
  </FrostedCard>
);

export default HardwareCard;
