import React from "react";

import AppTypography from "@/components/ui/AppTypography";
import { GAP_SM } from "@/theme/constants";

export interface CardIconHeaderProps {
  icon: React.ReactNode;
  /** Content rendered on the right side (chips, buttons, dropdowns…). */
  right?: React.ReactNode;
  style?: React.CSSProperties;
  subtitle?: React.ReactNode;
  title: React.ReactNode;
  /** Content rendered inline, immediately after the title. */
  titleSuffix?: React.ReactNode;
}

const CardIconHeader: React.FC<CardIconHeaderProps> = ({
  icon,
  title,
  subtitle,
  titleSuffix,
  right,
  style,
}) => (
  <div
    style={{
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      ...style,
    }}
  >
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: GAP_SM,
        minWidth: 0,
      }}
    >
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
        {icon}
      </div>
      <div style={{ minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: GAP_SM }}>
          <AppTypography
            fontWeight={700}
            noWrap
            style={{ lineHeight: 1.2 }}
            variant="subtitle1"
          >
            {title}
          </AppTypography>
          {titleSuffix}
        </div>
        {subtitle !== undefined && (
          <AppTypography color="text.secondary" noWrap variant="caption">
            {subtitle}
          </AppTypography>
        )}
      </div>
    </div>
    {right}
  </div>
);

export default CardIconHeader;
