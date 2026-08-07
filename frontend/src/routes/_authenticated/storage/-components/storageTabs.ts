import type { RoutedTab } from "@/components/tabbar";

export const STORAGE_TABS = [
  { label: "Disks", to: "/storage" },
  { label: "LVM", to: "/storage/lvm" },
] as const satisfies readonly RoutedTab[];
