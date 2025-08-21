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
  setHovered: (value: boolean) => void;
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

  // New: grab persisted collapsed state from config
  const [collapsed, setCollapsed] = useConfigValue("sidebarCollapsed");

  const [hovered, setHovered] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const hoverEnabled = useRef(true);

  const toggleCollapse = useCallback(() => {
    setCollapsed((prev) => {
      const next = !prev;
      if (isDesktop && next) {
        hoverEnabled.current = false;
        setHovered(false);
        setTimeout(() => {
          hoverEnabled.current = true;
        }, 200);
      }
      return next;
    });
  }, [isDesktop, setCollapsed]);

  const toggleMobileOpen = useCallback(() => {
    if (isDesktop) return;
    setMobileOpen((prev) => !prev);
  }, [isDesktop]);

  useEffect(() => {
    if (isDesktop) {
      setMobileOpen(false);
    } else {
      setHovered(false);
      setMobileOpen(false);
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
      setHovered,
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
      toggleCollapse,
      toggleMobileOpen,
    ],
  );

  return (
    <SidebarContext.Provider value={value}>{children}</SidebarContext.Provider>
  );
};
