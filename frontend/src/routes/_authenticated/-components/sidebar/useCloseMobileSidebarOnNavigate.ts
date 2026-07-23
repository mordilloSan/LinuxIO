import { useLocation } from "@tanstack/react-router";
import { useEffect } from "react";

import useSidebar from "./useSidebar";

/**
 * Closes the mobile sidebar drawer whenever the route changes.
 *
 * No-op on desktop, where the sidebar is always visible. Keyed on
 * `location.state.key` so repeat navigations to the same path still close it.
 */
export function useCloseMobileSidebarOnNavigate() {
  const location = useLocation();
  const { isDesktop, setMobileOpen } = useSidebar();

  useEffect(() => {
    if (!isDesktop) setMobileOpen(false);
  }, [location.state.key, isDesktop, setMobileOpen]);
}
