import type { ElementType } from "react";

export interface SidebarItemsType {
  href: string;
  icon?: ElementType | string;
  preload?: () => Promise<unknown>;
  preloadDelayMs?: number;
  title: string;
}
