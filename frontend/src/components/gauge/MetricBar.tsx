import { Box, Tooltip, Typography, LinearProgress } from "@mui/material";
import React from "react";

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
}) => (
  <Tooltip title={tooltip ?? ""}>
    <Box sx={{ width: "100%", mb: 1 }}>
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          mb: 0.5,
          px: 0.5,
        }}
      >
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.75 }}>
          {icon}
          <Typography variant="caption">{label}</Typography>
        </Box>
        <Typography
          variant="caption"
          sx={{ fontVariantNumeric: "tabular-nums" }}
        >
          {rightLabel}
        </Typography>
      </Box>
      <LinearProgress
        variant="determinate"
        value={percent}
        sx={{
          width: "100%",
          height: 7,
          borderRadius: 4,
          overflow: "hidden",
          backgroundColor: "rgba(255,255,255,0.09)",
          "& .MuiLinearProgress-bar": {
            backgroundColor: color,
            borderRadius: 4,
          },
        }}
      />
    </Box>
  </Tooltip>
);

export default MetricBar;
