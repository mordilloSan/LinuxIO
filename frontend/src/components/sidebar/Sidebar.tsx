import { Icon } from "@iconify/react";
import React, { useCallback, useState } from "react";

import SidebarNavList from "./SidebarNavList";
import LogoDisplay from "../logo/LogoDisplay";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import "./sidebar.css";
import { collapsedDrawerWidth, drawerWidth } from "@/constants";
import { useUpdateCanNavigate } from "@/hooks/useLinuxIOUpdater";
import useSidebar from "@/hooks/useSidebar";
import { useAppTheme } from "@/theme";
import { SidebarItemsType } from "@/types/sidebar";

export interface SidebarProps {
  items: SidebarItemsType[];
}

const Sidebar: React.FC<SidebarProps> = ({ items }) => {
  const theme = useAppTheme();
  const { collapsed, toggleCollapse, isDesktop, mobileOpen, setMobileOpen } =
    useSidebar();
  const canNavigate = useUpdateCanNavigate();

  const [hovered, setHovered] = useState(false);

  const effectiveWidth = !isDesktop
    ? drawerWidth
    : collapsed && !hovered
      ? collapsedDrawerWidth
      : drawerWidth;

  const handleMouseEnter = useCallback(() => {
    if (collapsed) setHovered(true);
  }, [collapsed]);
  const handleMouseLeave = useCallback(() => {
    if (collapsed) setHovered(false);
  }, [collapsed]);

  const showText = !collapsed || (hovered && isDesktop);
  const sidebarClassName = [
    "app-sidebar",
    isDesktop ? "app-sidebar--desktop" : "app-sidebar--mobile",
    mobileOpen && !isDesktop && "app-sidebar--open",
  ]
    .filter(Boolean)
    .join(" ");

  const sidebarStyle = {
    ["--sidebar-width" as string]: `${effectiveWidth}px`,
    width: effectiveWidth,
    transition: theme.transitions.create(["transform", "width"], {
      easing: theme.transitions.easing.easeInOut,
      duration: theme.transitions.duration.standard,
    }),
  } as React.CSSProperties;

  return (
    <>
      {!isDesktop && mobileOpen && (
        <button
          aria-label="Close navigation"
          className="app-sidebar-backdrop"
          onClick={() => setMobileOpen(false)}
          type="button"
        />
      )}
      <aside
        aria-label="Primary navigation"
        className={sidebarClassName}
        onMouseEnter={isDesktop ? handleMouseEnter : undefined}
        onMouseLeave={isDesktop ? handleMouseLeave : undefined}
        style={sidebarStyle}
      >
        <div className="app-sidebar__header">
          <LogoDisplay showText={showText} />

          {isDesktop && (!collapsed || (hovered && collapsed)) && (
            <AppTooltip title={collapsed ? "Expand" : "Collapse"}>
              <AppIconButton
                aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
                edge="end"
                onClick={toggleCollapse}
                size="small"
                style={{
                  position: "absolute",
                  right: 4,
                  top: "50%",
                  transform: "translateY(-50%)",
                }}
              >
                {!collapsed && (
                  <Icon height={22} icon="mdi:chevron-left" width={22} />
                )}
                {hovered && collapsed && (
                  <Icon height={22} icon="mdi:chevron-right" width={22} />
                )}
              </AppIconButton>
            </AppTooltip>
          )}
        </div>

        <nav className="app-sidebar__nav custom-scrollbar">
          <ul className="app-sidebar__list">
            {items.map((page) => (
              <SidebarNavList
                collapsed={isDesktop && collapsed && !hovered}
                disabled={!canNavigate}
                href={page.href}
                icon={page.icon}
                key={page.title}
                preload={page.preload}
                preloadDelayMs={page.preloadDelayMs}
                title={page.title}
              />
            ))}
          </ul>
        </nav>
      </aside>
    </>
  );
};

export default Sidebar;
