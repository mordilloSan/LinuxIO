import React from "react";

/**
 * Configuration for a single tab in the TabContainer
 */
export interface TabConfig {
  /** The component to render when this tab is active */
  component: React.ReactNode;
  /** Display label shown in the tab selector */
  label: string;
  /** Optional content to display in the right section of the TabSelector (e.g., action buttons) */
  rightContent?: React.ReactNode;
  /** Unique identifier for the tab */
  value: string;
}

/**
 * Props for the TabContainer component
 */
export interface TabContainerProps {
  /** Custom styles for the outer container */
  containerStyle?: React.CSSProperties;
  /** The default tab to show (used when no URL parameter is set) */
  defaultTab: string;
  /** Custom fallback UI to show when a tab component errors */
  errorFallback?: React.ReactNode;
  /** Duration of the fade animation in milliseconds (default: 300) */
  fadeTimeout?: number;
  /** Array of tab configurations */
  tabs: TabConfig[];
  /** Name of the URL query parameter to use for tab state (default: "tab") */
  urlParam?: string;
}
