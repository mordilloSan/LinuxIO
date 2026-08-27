import type { CSSProperties, ReactNode } from "react";

import "./MetricBar.css";

import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";

interface MetricBarProps {
  color: string;
  icon?: ReactNode;
  label: string;
  percent: number;
  rightLabel?: ReactNode;
  tooltip?: string;
}

const MetricBar = ({
  label,
  percent,
  color,
  tooltip,
  rightLabel,
  icon,
}: MetricBarProps) => {
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
          className="metric-bar-track"
          style={
            {
              width: "100%",
              height: 7,
              borderRadius: 4,
              overflow: "hidden",
              "--_lp-color": color,
            } as CSSProperties
          }
          value={percent}
          variant="determinate"
        />
      </div>
    </AppTooltip>
  );
};

export default MetricBar;
