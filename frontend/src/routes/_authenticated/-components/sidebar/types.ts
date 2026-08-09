import type { ComponentType } from "react";

import type { RouteTarget } from "@/types/navigation";

export type SidebarItem = RouteTarget & {
  icon?: ComponentType | string;
  title: string;
};
