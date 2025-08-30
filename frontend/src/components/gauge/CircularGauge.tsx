import { Box, Typography, CircularProgress, useTheme } from "@mui/material";
import { grey } from "@mui/material/colors";
import React, { useMemo } from "react";

interface CircularProgressWithLabelProps {
  value: number; // 0..100
  size?: number; // px
  thickness?: number; // px
}

function clamp(n: number, min: number, max: number) {
  return Math.min(max, Math.max(min, n));
}

// Linear interpolation helper
function lerp(a: number, b: number, t: number) {
  return a + (b - a) * t;
}

// Convert hex to RGB
function hexToRgb(hex: string) {
  const clean = hex.replace("#", "");
  const num = parseInt(clean, 16);
  return {
    r: (num >> 16) & 255,
    g: (num >> 8) & 255,
    b: num & 255,
  };
}

// Convert RGB back to CSS string
function rgbToCss({ r, g, b }: { r: number; g: number; b: number }) {
  return `rgb(${Math.round(r)}, ${Math.round(g)}, ${Math.round(b)})`;
}

function percentToColor(pct: number): string {
  const clamped = clamp(pct, 0, 100);
  // Start = #82ca9d (greenish), End = #e74c3c (red)
  const start = hexToRgb("#82ca9d");
  const end = hexToRgb("#e74c3c");

  const t = clamped / 100;
  return rgbToCss({
    r: lerp(start.r, end.r, t),
    g: lerp(start.g, end.g, t),
    b: lerp(start.b, end.b, t),
  });
}

const CircularProgressWithLabel: React.FC<CircularProgressWithLabelProps> = ({
  value,
  size = 100,
  thickness = 4,
}) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";
  const pct = clamp(Math.round(value), 0, 100);

  const ringColor = useMemo(() => percentToColor(pct), [pct]);

  return (
    <Box sx={{ position: "relative", display: "inline-flex" }}>
      {/* Background track */}
      <CircularProgress
        variant="determinate"
        value={100}
        size={size}
        thickness={thickness}
        sx={{
          position: "absolute",
          color: isDark ? grey[700] : grey[300],
        }}
      />
      {/* Foreground ring with dynamic color */}
      <CircularProgress
        variant="determinate"
        value={pct}
        size={size}
        thickness={thickness}
        sx={{
          color: ringColor,
          "& .MuiCircularProgress-circle": { strokeLinecap: "round" },
        }}
      />
      {/* Label */}
      <Box
        sx={{
          top: 0,
          left: 0,
          bottom: 0,
          right: 0,
          position: "absolute",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Typography variant="h6" color="text.primary">
          {pct}%
        </Typography>
      </Box>
    </Box>
  );
};

export default CircularProgressWithLabel;
