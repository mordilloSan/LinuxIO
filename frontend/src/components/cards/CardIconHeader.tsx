import React from "react";

import AppTypography from "@/components/ui/AppTypography";

export interface CardIconHeaderProps {
  icon: React.ReactNode;
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  /** Content rendered on the right side (chips, buttons, dropdowns…). */
  right?: React.ReactNode;
  /** Gap between icon box and text. Default 6. */
  gap?: number;
  style?: React.CSSProperties;
}

const CardIconHeader: React.FC<CardIconHeaderProps> = ({
  icon,
  title,
  subtitle,
  right,
  gap = 6,
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
    <div style={{ display: "flex", alignItems: "center", gap, minWidth: 0 }}>
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
        <AppTypography
          variant="subtitle1"
          fontWeight={700}
          style={{ lineHeight: 1.2 }}
          noWrap
        >
          {title}
        </AppTypography>
        {subtitle !== undefined && (
          <AppTypography variant="caption" color="text.secondary" noWrap>
            {subtitle}
          </AppTypography>
        )}
      </div>
    </div>
    {right}
  </div>
);

export default CardIconHeader;
