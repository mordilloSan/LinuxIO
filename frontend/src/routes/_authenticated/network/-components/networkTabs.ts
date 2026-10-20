import type { RoutedTab } from "@/components/tabbar";

export const NETWORK_TABS = [
  { label: "Interfaces", to: "/network" },
  { label: "WireGuard", to: "/network/wireguard" },
] as const satisfies readonly RoutedTab[];
