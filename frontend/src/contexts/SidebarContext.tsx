import React, { createContext, useState, useCallback, useMemo } from "react";

import { drawerWidth, collapsedDrawerWidth } from "@/constants";
import { useConfigValue } from "@/hooks/useConfig";
import breakpoints from "@/theme/breakpoints";
import { useAppMediaQuery } from "@/theme";

export interface SidebarContextType {
  collapsed: boolean;
  mobileOpen: boolean;
  isDesktop: boolean;
  sidebarWidth: number;
  setMobileOpen: (value: boolean) => void;
  toggleCollapse: () => void;
  toggleMobileOpen: () => void;
}

export const SidebarContext = createContext<SidebarContextType | undefined>(
  undefined,
);

export const SidebarProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const isDesktop = useAppMediaQuery(`(min-width:${breakpoints.values.md}px)`);

  // persisted collapsed flag
  const [collapsed, setCollapsed] = useConfigValue("sidebarCollapsed");
  const [mobileOpen, setMobileOpen] = useState(false);

  const toggleCollapse = useCallback(() => {
    setCollapsed((prev) => !prev);
  }, [setCollapsed]);

  const toggleMobileOpen = useCallback(() => {
    if (!isDesktop) setMobileOpen((prev) => !prev);
  }, [isDesktop]);

  // Sync mobile state with breakpoint - when isDesktop changes, close mobile menu
  const prevIsDesktop = React.useRef(isDesktop);
  React.useEffect(() => {
    if (prevIsDesktop.current !== isDesktop) {
      setMobileOpen(false);
      prevIsDesktop.current = isDesktop;
    }
  }, [isDesktop]);

  const sidebarWidth = useMemo(
    () =>
      isDesktop
        ? collapsed
          ? collapsedDrawerWidth
          : drawerWidth
        : drawerWidth,
    [isDesktop, collapsed],
  );

  const value = useMemo(
    () => ({
      collapsed,
      mobileOpen,
      isDesktop,
      sidebarWidth,
      setMobileOpen,
      toggleCollapse,
      toggleMobileOpen,
    }),
    [
      collapsed,
      mobileOpen,
      isDesktop,
      sidebarWidth,
      toggleCollapse,
      toggleMobileOpen,
    ],
  );

  return (
    <SidebarContext.Provider value={value}>{children}</SidebarContext.Provider>
  );
};
