import type { ReactNode } from "react";

interface TabPanelProps {
  children?: ReactNode;
  index: number;
  value: number;
}

export const TabPanel = ({ children, value, index }: TabPanelProps) => {
  return (
    <div
      hidden={value !== index}
      role="tabpanel"
      style={{
        paddingTop: "var(--app-space-8)",
        paddingBottom: "var(--app-space-8)",
        display: value === index ? "block" : "none",
      }}
    >
      {children}
    </div>
  );
};
