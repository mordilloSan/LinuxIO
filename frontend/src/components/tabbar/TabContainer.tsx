import { useMemo } from "react";

import type { TabContainerProps } from "@/types/tabcontainer";

import TabPanel from "./TabPanel";
import TabSelector from "./TabSelector";

import "./tab-container.css";

/**
 * TabContainer - A declarative component for managing tabbed interfaces
 *
 * Controlled tab layout. The owning route validates URL state and supplies
 * the selected tab plus its typed navigation callback.
 *
 * Provides:
 * - Lazy loading (tabs only mount when active)
 * - Per-tab action buttons in the tab bar
 *
 * @example
 * ```tsx
 * <TabContainer
 *   tabs={[
 *     {
 *       value: "users",
 *       label: "Users",
 *       component: <UsersPage />,
 *       rightContent: <Button>Action</Button>
 *     },
 *     {
 *       value: "groups",
 *       label: "Groups",
 *       component: <GroupsPage />
 *     }
 *   ]}
 *   activeTab={activeTab}
 *   onTabChange={setActiveTab}
 * />
 * ```
 */
const TabContainer = <TValue extends string>({
  activeTab,
  tabs,
  onTabChange,
  containerStyle = {},
}: TabContainerProps<TValue>) => {
  // Get the configuration for the currently active tab
  const activeTabConfig = useMemo(
    () => tabs.find((tab) => tab.value === activeTab),
    [activeTab, tabs],
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
        onChange={onTabChange}
        options={tabOptions}
        rightContent={activeTabConfig?.rightContent}
        value={activeTab}
      />

      {/* Only mount the active panel so large tab views do not overlap. */}
      <div className="tab-container__panels">
        {activeTabConfig && (
          <TabPanel key={activeTabConfig.value}>
            {activeTabConfig.component}
          </TabPanel>
        )}
      </div>
    </div>
  );
};

export default TabContainer;
