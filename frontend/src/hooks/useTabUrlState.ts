import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

/**
 * Custom hook for syncing tab state with URL query parameters
 *
 * @param defaultTab - The default tab value to use when no URL parameter is set
 * @param urlParam - The name of the URL query parameter (default: "tab")
 * @returns A tuple of [currentTab, setTab] similar to useState
 *
 * @example
 * const [activeTab, setActiveTab] = useTabUrlState("overview", "tab");
 * // URL: ?tab=details → activeTab = "details"
 * // URL: (no param) → activeTab = "overview"
 * setActiveTab("settings"); // Updates URL to ?tab=settings
 */
export function useTabUrlState(
  defaultTab: string,
  urlParam: string = "tab",
): [string, (newTab: string) => void] {
  const [searchParams, setSearchParams] = useSearchParams();

  // Get current tab from URL or use default
  const currentTab = searchParams.get(urlParam) || defaultTab;

  // Create setter function that updates URL
  const setTab = useCallback(
    (newTab: string) => {
      setSearchParams((prev) => {
        const newParams = new URLSearchParams(prev);
        newParams.set(urlParam, newTab);
        return newParams;
      });
    },
    [urlParam, setSearchParams],
  );

  return [currentTab, setTab];
}
