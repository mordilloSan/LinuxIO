import { createContext } from "react";

export interface SidebarContextType {
  collapsed: boolean;
  isDesktop: boolean;
  mobileOpen: boolean;
  setMobileOpen: (value: boolean) => void;
  sidebarWidth: number;
  toggleCollapse: () => void;
  toggleMobileOpen: () => void;
}

export const SidebarContext = createContext<SidebarContextType | undefined>(
  undefined,
);
