import { useAppTheme } from "@/theme";
import React from "react";

import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { alpha } from "@/utils/color";

interface MetricBarProps {
  label: string;
  percent: number;
  color: string;
  tooltip?: string;
  rightLabel?: React.ReactNode;
  icon?: React.ReactNode;
}

const MetricBar: React.FC<MetricBarProps> = ({
  label,
  percent,
  color,
  tooltip,
  rightLabel,
  icon,
}) => {
  const theme = useAppTheme();

  return (
    <AppTooltip title={tooltip ?? ""}>
      <div style={{ width: "100%", marginBottom: 4 }}>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            marginBottom: 2,
            paddingInline: 2,
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 3 }}>
            {icon}
            <AppTypography variant="caption">{label}</AppTypography>
          </div>
          <AppTypography
            variant="caption"
            style={{ fontVariantNumeric: "tabular-nums" }}
          >
            {rightLabel}
          </AppTypography>
        </div>
        <AppLinearProgress
          variant="determinate"
          value={percent}
          style={
            {
              width: "100%",
              height: 7,
              borderRadius: 4,
              overflow: "hidden",
              backgroundColor: alpha(
                theme.chart.neutral,
                theme.palette.mode === "dark" ? 0.18 : 0.12,
              ),
              "--_lp-color": color,
            } as React.CSSProperties
          }
        />
      </div>
    </AppTooltip>
  );
};

export default MetricBar;
