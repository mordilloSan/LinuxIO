import type { RoutedTab } from "@/components/tabbar";

export const STORAGE_TABS = [
  { label: "Disks", to: "/storage" },
  { label: "LVM", to: "/storage/lvm" },
  { label: "Btrfs", to: "/storage/btrfs" },
  { label: "Topology", to: "/storage/topology" },
  { label: "Shares", to: "/storage/shares" },
  { label: "Mounts", to: "/storage/mounts" },
  { label: "rsync", to: "/storage/rsync" },
] as const satisfies readonly RoutedTab[];
