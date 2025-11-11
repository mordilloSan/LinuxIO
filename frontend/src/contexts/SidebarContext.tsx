import { useMediaQuery, useTheme as useMuiTheme } from "@mui/material";
import React, {
  createContext,
  useState,
  useCallback,
  useMemo,
  useEffect,
  useRef,
} from "react";

import { drawerWidth, collapsedDrawerWidth } from "@/constants";
import { useConfigValue } from "@/hooks/useConfig";

export interface SidebarContextType {
  collapsed: boolean;
  hovered: boolean;
  mobileOpen: boolean;
  isDesktop: boolean;
  sidebarWidth: number;
  setHovered: (value: boolean) => void; // guarded
  setMobileOpen: (value: boolean) => void;
  toggleCollapse: () => void;
  toggleMobileOpen: () => void;
  hoverEnabledRef: React.RefObject<boolean>;
}

export const SidebarContext = createContext<SidebarContextType | undefined>(
  undefined,
);

export const SidebarProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const muiTheme = useMuiTheme();
  const isDesktop = useMediaQuery(muiTheme.breakpoints.up("md"));

  // persisted collapsed flag
  const [collapsed, setCollapsed] = useConfigValue("sidebarCollapsed");

  const [hovered, _setHovered] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);

  const hoverEnabled = useRef(true);
  const collapseTimer = useRef<number | null>(null);

  // Guarded setter so consumers don't need to read the ref
  const setHovered = useCallback((v: boolean) => {
    if (hoverEnabled.current) _setHovered(v);
  }, []);

  const toggleCollapse = useCallback(() => {
    setCollapsed((prev) => {
      const next = !prev;

      // When collapsing on desktop, temporarily disable hover to avoid flicker
      if (isDesktop && next) {
        hoverEnabled.current = false;
        _setHovered(false);

        if (collapseTimer.current) {
          window.clearTimeout(collapseTimer.current);
        }
        collapseTimer.current = window.setTimeout(() => {
          hoverEnabled.current = true;
          collapseTimer.current = null;
        }, 200);
      }
      return next;
    });
  }, [isDesktop, setCollapsed]);

  const toggleMobileOpen = useCallback(() => {
    if (!isDesktop) setMobileOpen((prev) => !prev);
  }, [isDesktop]);

  // Clean up pending timer
  useEffect(() => {
    return () => {
      if (collapseTimer.current) {
        window.clearTimeout(collapseTimer.current);
      }
    };
  }, []);

  // Sync mobile state with breakpoint - when isDesktop changes, close mobile menu
  const prevIsDesktop = React.useRef(isDesktop);
  React.useEffect(() => {
    if (prevIsDesktop.current !== isDesktop) {
      setMobileOpen(false);
      if (!isDesktop && collapsed) {
        _setHovered(false);
      }
      prevIsDesktop.current = isDesktop;
    }
  }, [isDesktop, collapsed]);

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
      hovered,
      mobileOpen,
      isDesktop,
      sidebarWidth,
      setHovered, // guarded setter
      setMobileOpen,
      toggleCollapse,
      toggleMobileOpen,
      hoverEnabledRef: hoverEnabled,
    }),
    [
      collapsed,
      hovered,
      mobileOpen,
      isDesktop,
      sidebarWidth,
      setHovered,
      toggleCollapse,
      toggleMobileOpen,
    ],
  );

  return (
    <SidebarContext.Provider value={value}>{children}</SidebarContext.Provider>
  );
};
