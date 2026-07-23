import { useNavigate, useSearch } from "@tanstack/react-router";
import { useCallback } from "react";

/**
 * Custom hook for syncing tab state with URL query parameters
 *
 * @param defaultTab - The default tab value to use when no URL parameter is set
 * @param urlParam - Required typed search key used to store the tab state
 * @returns A tuple of [currentTab, setTab] similar to useState
 *
 * @example
 * const [activeTab, setActiveTab] = useTabUrlState("users", "accountsTab");
 * // URL: ?accountsTab=groups → activeTab = "groups"
 * // URL: (no accountsTab) → activeTab = "users"
 * setActiveTab("groups"); // Updates URL to ?accountsTab=groups
 */
export type TabSearchKey =
  | "accountsTab"
  | "dockerTab"
  | "sharesTab"
  | "storageTab"
  | "updateTab"
  | "vmTab"
  | "section";

export function useTabUrlState(
  defaultTab: string,
  urlParam: TabSearchKey,
): [string, (newTab: string) => void] {
  const search = useSearch({ strict: false });
  const navigate = useNavigate();
  const current = search[urlParam];
  const currentTab =
    typeof current === "string" && current ? current : defaultTab;

  // Create setter function that updates URL
  const setTab = useCallback(
    (newTab: string) => {
      navigate({
        to: ".",
        search: (previous) => ({
          ...previous,
          [urlParam]: newTab,
        }),
      });
    },
    [navigate, urlParam],
  );

  return [currentTab, setTab];
}
