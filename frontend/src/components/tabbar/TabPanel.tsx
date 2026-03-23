import React, { useState } from "react";

import "./tab-panel.css";

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
  const [isPresent, setIsPresent] = useState(false);
  const shouldRender = isActive || isPresent;

  if (!shouldRender) {
    return null;
  }

  return (
    <div
      className="tab-panel"
      onAnimationStart={() => {
        if (isActive) {
          setIsPresent(true);
        }
      }}
      onAnimationEnd={() => {
        if (!isActive) {
          setIsPresent(false);
        }
      }}
      style={{
        animation: `${isActive ? "app-tab-panel-fade-in" : "app-tab-panel-fade-out"} ${timeout}ms ease forwards`,
        pointerEvents: isActive ? "auto" : "none",
      }}
    >
      <ErrorBoundary fallback={errorFallback}>{children}</ErrorBoundary>
    </div>
  );
};

export default TabPanel;
