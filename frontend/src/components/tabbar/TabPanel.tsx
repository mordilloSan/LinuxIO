import type { ReactNode } from "react";

import "./tab-panel.css";

import ErrorBoundary from "@/components/errors/ErrorBoundary";

interface TabPanelProps {
  /** The tab content to render */
  children: ReactNode;
}

/**
 * Internal layout and error boundary for the active tab panel.
 */
const TabPanel = ({ children }: TabPanelProps) => {
  return (
    <div className="tab-panel">
      <ErrorBoundary>
        <div className="tab-panel__content">{children}</div>
      </ErrorBoundary>
    </div>
  );
};

export default TabPanel;
