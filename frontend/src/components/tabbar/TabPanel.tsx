import { Box, Fade } from "@mui/material";
import React from "react";

import ErrorBoundary from "@/components/errors/ErrorBoundary";

interface TabPanelProps {
  /** The unique identifier for this tab */
  value: string;
  /** The currently active tab identifier */
  activeTab: string;
  /** Fade animation duration in milliseconds */
  timeout: number;
  /** Optional custom error fallback UI */
  errorFallback?: React.ReactNode;
  /** The tab content to render */
  children: React.ReactNode;
}

/**
 * Internal component that handles rendering of individual tab panels
 * with ErrorBoundary wrapping and fade animations.
 *
 * This component automatically:
 * - Wraps content in ErrorBoundary for error isolation
 * - Handles lazy mounting with unmountOnExit
 * - Provides smooth fade transitions
 * - Uses absolute positioning for proper animations
 */
const TabPanel: React.FC<TabPanelProps> = ({
  value,
  activeTab,
  timeout,
  errorFallback,
  children,
}) => {
  const isActive = activeTab === value;

  return (
    <Fade in={isActive} timeout={timeout} unmountOnExit={true}>
      <Box
        sx={{
          position: "absolute",
          width: "100%",
          top: 0,
          left: 0,
        }}
      >
        <ErrorBoundary fallback={errorFallback}>{children}</ErrorBoundary>
      </Box>
    </Fade>
  );
};

export default TabPanel;
