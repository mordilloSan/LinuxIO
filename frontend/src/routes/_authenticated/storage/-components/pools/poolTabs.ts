import type { SearchInput } from "@/routes/-search";

// The pool page's tabs and the ?tab= values they put in the URL, so the
// selector and the route's validator cannot drift apart. Kept apart from the
// page so the route can validate eagerly without pulling the page out of its
// lazy chunk.
export const POOL_TABS = [
  { label: "Overview", value: "overview" },
  { label: "Snapshots", value: "snapshots" },
  { label: "Disks", value: "disks" },
  { label: "Settings", value: "settings" },
] as const;

export type PoolTab = (typeof POOL_TABS)[number]["value"];

export const DEFAULT_POOL_TAB: PoolTab = "overview";

/** An unknown tab is dropped, so the page falls back to Overview. */
export const validatePoolTab = (search: SearchInput): { tab?: PoolTab } =>
  POOL_TABS.some((tab) => tab.value === search.tab)
    ? { tab: search.tab as PoolTab }
    : {};
