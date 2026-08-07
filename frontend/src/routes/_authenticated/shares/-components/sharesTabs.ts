import type { RoutedTab } from "@/components/tabbar";

export const SHARES_TABS = [
  { label: "Shares", to: "/shares" },
  { label: "Mounts", to: "/shares/mounts" },
] as const satisfies readonly RoutedTab[];
