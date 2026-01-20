import BuildIcon from "@mui/icons-material/Build";
import { Box, Typography } from "@mui/material";
import React from "react";

import { DevToolsPanel } from "@/components/dev-tools/DevToolsPanel";

const DevToolsButton: React.FC = () => {
  const [isOpen, setIsOpen] = React.useState(false);

  // Only show in development mode
  if (!import.meta.env.DEV) {
    return null;
  }

  return (
    <>
      <Box sx={{ position: "relative", display: "inline-flex" }}>
        <Box
          role="button"
          tabIndex={0}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              setIsOpen((prev) => !prev);
            }
          }}
          onClick={() => setIsOpen((prev) => !prev)}
          sx={{
            cursor: "pointer",
            display: "flex",
            alignItems: "center",
            gap: 0.75,
            border: "1px solid",
            borderColor: isOpen ? "primary.main" : "transparent",
            borderRadius: 1,
            p: 1,
            boxShadow: isOpen ? 2 : "none",
            whiteSpace: "nowrap",
            minWidth: 90,
            transition: "all 0.2s",
            "&:hover": {
              borderColor: "primary.main",
              boxShadow: 1,
            },
          }}
        >
          <BuildIcon sx={{ fontSize: 16, color: "primary.main" }} />
          <Typography variant="caption" color="text.secondary">
            Dev Tools
          </Typography>
        </Box>
      </Box>
      <DevToolsPanel isOpen={isOpen} onClose={() => setIsOpen(false)} />
    </>
  );
};

export default DevToolsButton;
