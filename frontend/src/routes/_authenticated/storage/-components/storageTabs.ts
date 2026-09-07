import type { RoutedTab } from "@/components/tabbar";

export const STORAGE_TABS = [
  { label: "Disks", to: "/storage" },
  { label: "LVM", to: "/storage/lvm" },
  { label: "Topology", to: "/storage/topology" },
] as const satisfies readonly RoutedTab[];
