import type { CSSProperties, ReactNode } from "react";

import type { TabSearchKey } from "@/hooks/useTabUrlState";

/**
 * Configuration for a single tab in the TabContainer
 */
export interface TabConfig {
  /** The component to render when this tab is active */
  component: ReactNode;
  /** Display label shown in the tab selector */
  label: string;
  /** Optional content to display in the right section of the TabSelector (e.g., action buttons) */
  rightContent?: ReactNode;
  /** Unique identifier for the tab */
  value: string;
}

/**
 * Props for the TabContainer component
 */
export interface TabContainerProps {
  /** Custom styles for the outer container */
  containerStyle?: CSSProperties;
  /** The default tab to show (used when no URL parameter is set) */
  defaultTab: string;
  /** Custom fallback UI to show when a tab component errors */
  errorFallback?: ReactNode;
  /** Array of tab configurations */
  tabs: TabConfig[];
  /** Required URL search key, limited to the supported typed tab keys */
  urlParam: TabSearchKey;
}
