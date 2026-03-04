import BuildIcon from "@mui/icons-material/Build";
import { Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import "@/components/cards/frosted-card.css";
import { DevToolsPanel } from "@/components/dev-tools/DevToolsPanel";

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
              boxShadow: isOpen ? theme.shadows[2] : "none",
              whiteSpace: "nowrap",
              minWidth: 90,
              transition: "all 0.2s",
              "--devtools-hover-border": theme.palette.primary.main,
              "--devtools-hover-shadow": theme.shadows[1],
            } as React.CSSProperties
          }
        >
          <BuildIcon sx={{ fontSize: 16, color: "primary.main" }} />
          <Typography variant="caption" color="text.secondary">
            Dev Tools
          </Typography>
        </div>
      </div>
      <DevToolsPanel isOpen={isOpen} onClose={() => setIsOpen(false)} />
    </>
  );
};

export default React.memo(DevToolsButton);
