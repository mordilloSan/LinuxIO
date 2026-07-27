import type { RoutedTab } from "@/components/tabbar";

export const VM_TABS = [
  { label: "Global dashboard", to: "/vm" },
  { label: "Networks", to: "/vm/networks" },
  { label: "Images", to: "/vm/images" },
  { label: "Virtual machines", to: "/vm/machines" },
] as const satisfies readonly RoutedTab[];
