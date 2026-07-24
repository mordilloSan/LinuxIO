import type { CSSProperties, ReactNode } from "react";

/**
 * Configuration for a single tab in the TabContainer
 */
export interface TabConfig<TValue extends string = string> {
  /** The component to render when this tab is active */
  component: ReactNode;
  /** Display label shown in the tab selector */
  label: string;
  /** Optional content to display in the right section of the TabSelector (e.g., action buttons) */
  rightContent?: ReactNode;
  /** Unique identifier for the tab */
  value: TValue;
}

/**
 * Props for the TabContainer component
 */
export interface TabContainerProps<TValue extends string = string> {
  /** Validated value supplied by the owning route. */
  activeTab: TValue;
  /** Custom styles for the outer container */
  containerStyle?: CSSProperties;
  /** Route-typed navigation callback supplied by the owning route. */
  onTabChange: (newTab: TValue) => void;
  /** Array of tab configurations */
  tabs: readonly TabConfig<TValue>[];
}
