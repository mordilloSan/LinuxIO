import React from "react";

import { useAppTheme } from "@/theme";

interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

export const TabPanel = ({ children, value, index }: TabPanelProps) => {
  const theme = useAppTheme();

  return (
    <div
      hidden={value !== index}
      role="tabpanel"
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
