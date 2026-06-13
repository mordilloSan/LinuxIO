import React from "react";

export interface SidebarItemsType {
  href: string;
  icon?: React.ElementType | string;
  preload?: () => Promise<unknown>;
  title: string;
}
