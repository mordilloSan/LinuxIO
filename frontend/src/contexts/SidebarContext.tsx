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
import useAppTheme from "@/hooks/useAppTheme";

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
  const { sidebarColapsed: collapsed, setSidebarColapsed } = useAppTheme();

  const [hovered, setHovered] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const hoverEnabled = useRef(true);

  // Toggle collapsed state, temporarily disables hover to avoid "double triggers"
  const toggleCollapse = useCallback(() => {
    setSidebarColapsed((prev) => {
      const newState = !prev;
      if (isDesktop && newState) {
        hoverEnabled.current = false;
        setHovered(false);
        setTimeout(() => {
          hoverEnabled.current = true;
        }, 200);
      }
      return newState;
    });
  }, [isDesktop, setSidebarColapsed]);

  // Toggle mobile drawer
  const toggleMobileOpen = useCallback(() => {
    if (isDesktop) return;
    setMobileOpen((prev) => !prev);
  }, [isDesktop]);

  // Sync UI state on breakpoint or collapse state changes
  useEffect(() => {
    if (isDesktop) {
      setMobileOpen(false);
    } else {
      setHovered(false);
      setMobileOpen(false);
    }
  }, [isDesktop, collapsed]);

  // Responsive sidebar width
  const sidebarWidth = useMemo(() => {
    return isDesktop
      ? collapsed
        ? collapsedDrawerWidth
        : drawerWidth
      : drawerWidth;
  }, [isDesktop, collapsed]);

  // Memoize context value
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
