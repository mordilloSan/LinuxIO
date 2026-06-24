import React from "react";

import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface MetricBarProps {
  color: string;
  icon?: React.ReactNode;
  label: string;
  percent: number;
  rightLabel?: React.ReactNode;
  tooltip?: string;
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
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 3,
              minWidth: 0,
              flex: 1,
            }}
          >
            {icon}
            <AppTypography noWrap style={{ minWidth: 0 }} variant="caption">
              {label}
            </AppTypography>
          </div>
          <AppTypography
            noWrap
            style={{
              fontVariantNumeric: "tabular-nums",
              flexShrink: 0,
              marginLeft: 6,
            }}
            variant="caption"
          >
            {rightLabel}
          </AppTypography>
        </div>
        <AppLinearProgress
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
          value={percent}
          variant="determinate"
        />
      </div>
    </AppTooltip>
  );
};

export default MetricBar;
