import { useMemo } from "react";

import { useTabUrlState } from "@/hooks/useTabUrlState";
import { TabContainerProps } from "@/types/tabcontainer";

import TabPanel from "./TabPanel";
import TabSelector from "./TabSelector";

import "./tab-container.css";

/**
 * TabContainer - A declarative component for managing tabbed interfaces
 *
 * Provides automatic:
 * - URL query parameter persistence (tab state survives page reload)
 * - Lazy loading (tabs only mount when active)
 * - Error boundary wrapping (errors in one tab don't crash others)
 * - Per-tab action buttons in the tab bar
 *
 * @example
 * ```tsx
 * <TabContainer
 *   tabs={[
 *     {
 *       value: "overview",
 *       label: "Overview",
 *       component: <OverviewPage />,
 *       rightContent: <Button>Action</Button>
 *     },
 *     {
 *       value: "details",
 *       label: "Details",
 *       component: <DetailsPage />
 *     }
 *   ]}
 *   defaultTab="overview"
 *   urlParam="view"
 * />
 * ```
 */
const TabContainer = ({
  tabs,
  defaultTab,
  urlParam = "tab",
  containerStyle = {},
  errorFallback,
}: TabContainerProps) => {
  // Sync tab state with URL query parameter
  const [activeTab, setActiveTab] = useTabUrlState(defaultTab, urlParam);

  // Validate that activeTab exists in tabs array, fallback to default if invalid
  const validTab = useMemo(() => {
    const isValid = tabs.some((tab) => tab.value === activeTab);
    return isValid ? activeTab : defaultTab;
  }, [activeTab, tabs, defaultTab]);

  // Get the configuration for the currently active tab
  const activeTabConfig = useMemo(
    () => tabs.find((tab) => tab.value === validTab),
    [tabs, validTab],
  );

  // Build options array for TabSelector component
  const tabOptions = useMemo(
    () => tabs.map((tab) => ({ value: tab.value, label: tab.label })),
    [tabs],
  );

  return (
    <div className="tab-container" style={containerStyle}>
      {/* Tab selector with optional rightContent from active tab */}
      <TabSelector
        className="tab-container__selector"
        onChange={setActiveTab}
        options={tabOptions}
        rightContent={activeTabConfig?.rightContent}
        value={validTab}
      />

      {/* Only mount the active panel so large tab views do not overlap. */}
      <div className="tab-container__panels">
        {activeTabConfig && (
          <TabPanel
            errorFallback={errorFallback}
            key={activeTabConfig.value}
          >
            {activeTabConfig.component}
          </TabPanel>
        )}
      </div>
    </div>
  );
};

export default TabContainer;
