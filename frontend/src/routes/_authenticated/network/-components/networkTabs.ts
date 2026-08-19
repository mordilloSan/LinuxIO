import type { RoutedTab } from "@/components/tabbar";

export const NETWORK_TABS = [
  { label: "Interfaces", to: "/network" },
] as const satisfies readonly RoutedTab[];
