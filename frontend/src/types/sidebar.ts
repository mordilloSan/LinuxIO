import React from "react";

export interface SidebarItemsType {
  href: string;
  icon?: React.ElementType | string;
  preload?: () => Promise<unknown>;
  preloadDelayMs?: number;
  title: string;
}
