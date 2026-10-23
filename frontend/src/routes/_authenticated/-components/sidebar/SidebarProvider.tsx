import { useCallback, useMemo, useState, type ReactNode } from "react";

import { useConfigValue } from "@/hooks/useConfig";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import breakpoints from "@/theme/breakpoints";

import { SidebarContext } from "./SidebarContext";
import { collapsedDrawerWidth, drawerWidth } from "./sidebarLayout";

export const SidebarProvider = ({ children }: { children: ReactNode }) => {
  const isDesktop = useMediaQuery(`(min-width:${breakpoints.values.md}px)`);

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
