import { Box, Typography, CircularProgress, useTheme } from "@mui/material";
import { grey } from "@mui/material/colors";
import React from "react";

interface CircularProgressWithLabelProps {
  value: number;
  size?: number;
  thickness?: number;
  color?: "primary" | "secondary" | "error" | "success" | "warning" | "info";
}

const CircularProgressWithLabel: React.FC<CircularProgressWithLabelProps> = ({
  value,
  size = 100,
  thickness = 4,
  color = "primary",
}) => {
  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";

  return (
    <Box
      sx={{
        position: "relative",
        display: "inline-flex",
        ml: { xs: 0, sm: 0, xl: 0 },
      }}
    >
      {/* Background circle */}
      <CircularProgress
        variant="determinate"
        value={100}
        size={size}
        thickness={thickness}
        sx={{
          position: "absolute",
          color: isDark ? grey[600] : grey[300],
        }}
      />
      {/* Foreground circle */}
      <CircularProgress
        variant="determinate"
        value={value}
        size={size}
        thickness={thickness}
        color={color}
        sx={{
          "& .MuiCircularProgress-circle": {
            strokeLinecap: "round",
          },
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
          {`${Math.round(value)}%`}
        </Typography>
      </Box>
    </Box>
  );
};

export default CircularProgressWithLabel;
