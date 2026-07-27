import type { RoutedTab } from "@/components/tabbar";

export const DOCKER_TABS = [
  { label: "Dashboard", to: "/docker" },
  { label: "Containers", to: "/docker/containers" },
  { label: "Stacks", to: "/docker/compose" },
  { label: "Networks", to: "/docker/networks" },
  { label: "Volumes", to: "/docker/volumes" },
  { label: "Images", to: "/docker/images" },
] as const satisfies readonly RoutedTab[];
