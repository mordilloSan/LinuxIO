import { Box, Typography } from "@mui/material";
import React from "react";

interface InfoRowProps {
  label: string;
  value: React.ReactNode;
  valueColor?: string;
}

export const InfoRow: React.FC<InfoRowProps> = ({
  label,
  value,
  valueColor,
}) => (
  <Box
    sx={{
      display: "flex",
      justifyContent: "space-between",
      py: 1,
      borderBottom: "1px solid",
      borderColor: "divider",
    }}
  >
    <Typography variant="body2" color="text.secondary">
      {label}
    </Typography>
    <Typography
      variant="body2"
      fontWeight={500}
      color={valueColor || "text.primary"}
    >
      {value}
    </Typography>
  </Box>
);
