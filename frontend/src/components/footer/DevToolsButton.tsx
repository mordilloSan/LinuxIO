import { Icon } from "@iconify/react";
import { useTheme } from "@mui/material/styles";
import { shadowSm } from "@/constants";
import React from "react";

import "@/components/cards/frosted-card.css";
import { DevToolsPanel } from "@/components/dev-tools/DevToolsPanel";
import AppTypography from "@/components/ui/AppTypography";

const DevToolsButton: React.FC = () => {
  const theme = useTheme();
  const [isOpen, setIsOpen] = React.useState(false);

  // Only show in development mode
  if (!import.meta.env.DEV) {
    return null;
  }

  return (
    <>
      <div style={{ position: "relative", display: "inline-flex" }}>
        <div
          role="button"
          tabIndex={0}
          className="devtools-btn"
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              setIsOpen((prev) => !prev);
            }
          }}
          onClick={() => setIsOpen((prev) => !prev)}
          style={
            {
              cursor: "pointer",
              display: "flex",
              alignItems: "center",
              gap: 3,
              border: "1px solid",
              borderColor: isOpen ? theme.palette.primary.main : "transparent",
              borderRadius: 4,
              padding: 4,
              boxShadow: isOpen ? shadowSm : "none",
              whiteSpace: "nowrap",
              minWidth: 90,
              transition: "all 0.2s",
              "--devtools-hover-border": theme.palette.primary.main,
              "--devtools-hover-shadow": shadowSm,
            } as React.CSSProperties
          }
        >
          <Icon
            icon="mdi:wrench"
            width={16}
            height={16}
            style={{ color: theme.palette.primary.main }}
          />
          <AppTypography variant="caption" color="text.secondary">
            Dev Tools
          </AppTypography>
        </div>
      </div>
      <DevToolsPanel isOpen={isOpen} onClose={() => setIsOpen(false)} />
    </>
  );
};

export default React.memo(DevToolsButton);
