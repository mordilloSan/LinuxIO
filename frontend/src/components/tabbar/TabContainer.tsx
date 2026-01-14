import { Box } from "@mui/material";
import React, { useMemo } from "react";

import TabPanel from "./TabPanel";
import TabSelector from "./TabSelector";

import { useTabUrlState } from "@/hooks/useTabUrlState";
import { TabContainerProps } from "@/types/tabcontainer";

/**
 * TabContainer - A declarative component for managing tabbed interfaces
 *
 * Provides automatic:
 * - URL query parameter persistence (tab state survives page reload)
 * - Lazy loading (tabs only mount when active)
 * - Error boundary wrapping (errors in one tab don't crash others)
 * - Fade animations between tabs
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
const TabContainer: React.FC<TabContainerProps> = ({
  tabs,
  defaultTab,
  urlParam = "tab",
  fadeTimeout = 300,
  containerSx = { px: 2 },
  errorFallback,
}) => {
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
    <Box sx={containerSx}>
      {/* Tab selector with optional rightContent from active tab */}
      <TabSelector
        value={validTab}
        onChange={setActiveTab}
        options={tabOptions}
        rightContent={activeTabConfig?.rightContent}
      />

      {/* Container for tab panels with relative positioning for absolute children */}
      <Box sx={{ position: "relative", minHeight: 400 }}>
        {tabs.map((tab) => (
          <TabPanel
            key={tab.value}
            value={tab.value}
            activeTab={validTab}
            timeout={fadeTimeout}
            errorFallback={errorFallback}
          >
            {tab.component}
          </TabPanel>
        ))}
      </Box>
    </Box>
  );
};

export default TabContainer;
