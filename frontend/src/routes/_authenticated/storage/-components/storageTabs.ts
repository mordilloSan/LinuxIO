import type { RoutedTabEntry } from "@/components/tabbar";

export const STORAGE_TABS = [
  { label: "Overview", to: "/storage" },
  { label: "Pools", matchChildren: true, to: "/storage/pools" },
  { label: "Shared folders", to: "/storage/shares" },
  { label: "Snapshots", to: "/storage/snapshots" },
  {
    label: "Advanced",
    items: [
      { label: "Disks", to: "/storage/disks" },
      { label: "LVM", to: "/storage/lvm" },
      { label: "Mounts", to: "/storage/mounts" },
      { label: "rsync", to: "/storage/rsync" },
    ],
  },
] as const satisfies readonly RoutedTabEntry[];
