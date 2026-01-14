import { SxProps } from "@mui/material";
import React from "react";

/**
 * Configuration for a single tab in the TabContainer
 */
export interface TabConfig {
  /** Unique identifier for the tab */
  value: string;
  /** Display label shown in the tab selector */
  label: string;
  /** The component to render when this tab is active */
  component: React.ReactNode;
  /** Optional content to display in the right section of the TabSelector (e.g., action buttons) */
  rightContent?: React.ReactNode;
}

/**
 * Props for the TabContainer component
 */
export interface TabContainerProps {
  /** Array of tab configurations */
  tabs: TabConfig[];
  /** The default tab to show (used when no URL parameter is set) */
  defaultTab: string;
  /** Name of the URL query parameter to use for tab state (default: "tab") */
  urlParam?: string;
  /** Duration of the fade animation in milliseconds (default: 300) */
  fadeTimeout?: number;
  /** Custom styles for the outer container */
  containerSx?: SxProps;
  /** Custom fallback UI to show when a tab component errors */
  errorFallback?: React.ReactNode;
}
