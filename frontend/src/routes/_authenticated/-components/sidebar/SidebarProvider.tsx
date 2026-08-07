import { useCallback, useMemo, useState, type ReactNode } from "react";

import { useConfigValue } from "@/hooks/useConfig";
import { useAppMediaQuery } from "@/theme";
import breakpoints from "@/theme/breakpoints";
import { collapsedDrawerWidth, drawerWidth } from "@/theme/constants";

import { SidebarContext } from "./SidebarContext";

export const SidebarProvider = ({ children }: { children: ReactNode }) => {
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

  // Sync mobile state with breakpoint: when crossing to desktop, close the mobile
  // menu. Adjusting state during render (instead of in an effect) closes it before
  // paint, avoiding a frame where the drawer lingers after the breakpoint flips.
  const [prevIsDesktop, setPrevIsDesktop] = useState(isDesktop);
  if (prevIsDesktop !== isDesktop) {
    setPrevIsDesktop(isDesktop);
    setMobileOpen(false);
  }

  const sidebarWidth =
    isDesktop && collapsed ? collapsedDrawerWidth : drawerWidth;

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
