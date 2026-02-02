import { Box } from "@mui/material";
import React from "react";

interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

export const TabPanel: React.FC<TabPanelProps> = ({
  children,
  value,
  index,
}) => {
  return (
    <Box
      role="tabpanel"
      hidden={value !== index}
      sx={{ py: 2, display: value === index ? "block" : "none" }}
    >
      {children}
    </Box>
  );
};
