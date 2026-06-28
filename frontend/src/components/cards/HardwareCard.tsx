import { Icon } from "@iconify/react";
import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
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
            color="text.secondary"
            style={{
              textTransform: "uppercase",
              flexShrink: 0,
            }}
            variant="caption"
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
                align="right"
                fontWeight={500}
                noWrap={noWrap ?? true}
                style={{ width: "100%", textAlign: "right" }}
                variant="body2"
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
    hoverLift
    style={{
      display: "flex",
      flexDirection: "column",
      boxSizing: "border-box",
      height: "100%",
      padding: 8,
    }}
  >
    <CardIconHeader
      icon={
        <Icon color={accentColor} height={28} icon={avatarIcon} width={28} />
      }
      right={
        actions ? (
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
        ) : undefined
      }
      style={{ marginBottom: 6 }}
      subtitle={subtitle}
      title={title}
    />

    <div style={{ display: "flex", flex: 1, minWidth: 0 }}>
      <div
        style={{
          flex: 1,
          minWidth: 0,
          display: "flex",
          alignItems: "stretch",
        }}
      >
        <SummaryRowsList rows={rows} />
      </div>
    </div>
  </FrostedCard>
);

export default HardwareCard;
