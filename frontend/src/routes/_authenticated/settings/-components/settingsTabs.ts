import type { SearchInput } from "@/routes/-search";

// The tab strip's contents and the ?tab= values it puts in the URL, so the
// selector's labels and the route's validator cannot drift apart.
export const SETTINGS_TABS = [
  { label: "General", value: "general" },
  { label: "Updates", value: "updates" },
  { label: "Theme", value: "theme" },
  { label: "Capabilities", value: "capabilities" },
  { label: "Docker", value: "docker" },
  { label: "Indexer", value: "indexer" },
  { label: "Monitoring", value: "monitoring" },
  { label: "Power", value: "power" },
] as const;

export type SettingsTab = (typeof SETTINGS_TABS)[number]["value"];

/** Sections an unprivileged session cannot read. Their pills stay hidden. */
export const PRIVILEGED_SETTINGS_TABS: readonly SettingsTab[] = [
  "indexer",
  "monitoring",
  "power",
];

export const DEFAULT_SETTINGS_TAB: SettingsTab = "general";

/* Kept in its own module rather than beside the page: the route needs it
   eagerly for validateSearch, and importing it from SettingsPage would pull
   that component — and every settings section under it — out of the lazy
   chunk autoCodeSplitting puts it in. */
export const validateSettingsTab = (
  search: SearchInput,
): { tab?: SettingsTab } => {
  const value = search.tab;
  // An unrecognized tab is dropped instead of rejected, matching the page's own
  // fall back to General for a tab this session may not see.
  return SETTINGS_TABS.some((tab) => tab.value === value)
    ? { tab: value as SettingsTab }
    : {};
};
