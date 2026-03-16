import { LinearProgress } from "@mui/material";
import { useTheme } from "@mui/material/styles";

import AppTypography from "@/components/ui/AppTypography";
import { alpha } from "@/utils/color";
import React from "react";

import AppTooltip from "@/components/ui/AppTooltip";

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
  const theme = useTheme();

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
        <LinearProgress
          variant="determinate"
          value={percent}
          sx={{
            width: "100%",
            height: 7,
            borderRadius: 4,
            overflow: "hidden",
            backgroundColor: alpha(
              theme.chart.neutral,
              theme.palette.mode === "dark" ? 0.18 : 0.12,
            ),
            "& .MuiLinearProgress-bar": {
              backgroundColor: color,
              borderRadius: 4,
            },
          }}
        />
      </div>
    </AppTooltip>
  );
};

export default MetricBar;
