import type { RoutedTab } from "@/components/tabbar";

export const STORAGE_TABS = [
  { label: "Disks", to: "/storage" },
  { label: "LVM", to: "/storage/lvm" },
  { label: "Subvolumes", to: "/storage/subvolumes" },
  { label: "Shares", to: "/storage/shares" },
  { label: "Mounts", to: "/storage/mounts" },
  { label: "rsync", to: "/storage/rsync" },
] as const satisfies readonly RoutedTab[];
