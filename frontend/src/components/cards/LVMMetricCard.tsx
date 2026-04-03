import { Icon } from "@iconify/react";
import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";

export interface LVMMetricCardProps {
  title: string;
  value: string;
  subtitle: string;
  icon: string;
  color: string;
}

const LVMMetricCard: React.FC<LVMMetricCardProps> = ({
  title,
  value,
  subtitle,
  icon,
  color,
}) => (
  <FrostedCard style={{ padding: 14, minWidth: 0 }}>
    <div
      style={{
        display: "flex",
        alignItems: "flex-start",
        justifyContent: "space-between",
        gap: 12,
      }}
    >
      <div style={{ minWidth: 0 }}>
        <AppTypography
          variant="caption"
          color="text.secondary"
          style={{ textTransform: "uppercase", letterSpacing: "0.08em" }}
        >
          {title}
        </AppTypography>
        <AppTypography
          variant="subtitle1"
          fontWeight={800}
          style={{ marginTop: 4, marginBottom: 4 }}
        >
          {value}
        </AppTypography>
        <AppTypography variant="body2" color="text.secondary">
          {subtitle}
        </AppTypography>
      </div>
      <div
        style={{
          width: 42,
          height: 42,
          borderRadius: 14,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          color,
          background: `color-mix(in srgb, ${color} 16%, transparent)`,
          border: `1px solid color-mix(in srgb, ${color} 30%, transparent)`,
          flexShrink: 0,
        }}
      >
        <Icon icon={icon} width={22} height={22} />
      </div>
    </div>
  </FrostedCard>
);

export default LVMMetricCard;
