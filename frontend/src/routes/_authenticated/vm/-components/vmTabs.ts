import type { RoutedTab } from "@/components/tabbar";

export const VM_TABS = [
  { label: "Global dashboard", to: "/vm" },
  { label: "Networks", to: "/vm/networks" },
  { label: "Images", to: "/vm/images" },
  // Owns child detail routes (/vm/machines/$name), so the pill must stay
  // selected while a machine is open.
  { label: "Virtual machines", matchChildren: true, to: "/vm/machines" },
] as const satisfies readonly RoutedTab[];
