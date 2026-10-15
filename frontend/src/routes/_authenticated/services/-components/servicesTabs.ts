import type { RoutedTab } from "@/components/tabbar";

export const SERVICES_TABS = [
  { label: "Services", to: "/services" },
  { label: "Timers", to: "/services/timers" },
  { label: "Sockets", to: "/services/sockets" },
  { label: "Scheduled tasks", to: "/services/schedules" },
  { label: "Processes", to: "/services/processes" },
  { label: "Programs", to: "/services/programs" },
  { label: "htop", to: "/services/htop" },
] as const satisfies readonly RoutedTab[];
