import type { RoutedTab } from "@/components/tabbar";

export const ACCOUNTS_TABS = [
  { label: "Users", to: "/accounts" },
  { label: "Groups", to: "/accounts/groups" },
] as const satisfies readonly RoutedTab[];
