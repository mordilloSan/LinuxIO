import type { ComponentType } from "react";

import type { RouteTarget } from "@/api";

export type SidebarItem = RouteTarget & {
  icon?: ComponentType | string;
  title: string;
};
