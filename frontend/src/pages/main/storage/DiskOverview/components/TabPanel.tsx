import { useTheme } from "@mui/material/styles";
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
  const theme = useTheme();

  return (
    <div
      role="tabpanel"
      hidden={value !== index}
      style={{
        paddingTop: theme.spacing(2),
        paddingBottom: theme.spacing(2),
        display: value === index ? "block" : "none",
      }}
    >
      {children}
    </div>
  );
};
