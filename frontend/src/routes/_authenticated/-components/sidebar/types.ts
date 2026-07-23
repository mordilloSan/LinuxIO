import type { ElementType } from "react";

import type { RouteTarget } from "@/types/navigation";

export type SidebarItem = RouteTarget & {
  icon?: ElementType | string;
  title: string;
};
