import type { RoutedTab } from "@/components/tabbar";

export const UPDATES_TABS = [
  { label: "Updates", to: "/updates" },
  { label: "History", to: "/updates/history" },
] as const satisfies readonly RoutedTab[];
