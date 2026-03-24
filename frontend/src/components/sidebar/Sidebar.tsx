import { Icon } from "@iconify/react";
import React, { useState, useCallback } from "react";

import SidebarNavList from "./SidebarNavList";
import LogoDisplay from "../logo/LogoDisplay";
import "./sidebar.css";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { collapsedDrawerWidth, drawerWidth } from "@/constants";
import { useLinuxIOUpdater } from "@/hooks/useLinuxIOUpdater";
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
  const { canNavigate } = useLinuxIOUpdater();

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
      easing: theme.transitions.easing.sharp,
      duration: theme.transitions.duration.standard,
    }),
  } as React.CSSProperties;

  return (
    <>
      {!isDesktop && mobileOpen && (
        <button
          type="button"
          className="app-sidebar-backdrop"
          aria-label="Close navigation"
          onClick={() => setMobileOpen(false)}
        />
      )}
      <aside
        className={sidebarClassName}
        style={sidebarStyle}
        aria-label="Primary navigation"
        onMouseEnter={isDesktop ? handleMouseEnter : undefined}
        onMouseLeave={isDesktop ? handleMouseLeave : undefined}
      >
        <div className="app-sidebar__header">
          <LogoDisplay showText={showText} />

          {isDesktop && (!collapsed || (hovered && collapsed)) && (
            <AppTooltip title={collapsed ? "Expand" : "Collapse"}>
              <AppIconButton
                onClick={toggleCollapse}
                size="small"
                style={{
                  position: "absolute",
                  right: 4,
                  top: "50%",
                  transform: "translateY(-50%)",
                }}
                aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
                edge="end"
              >
                {!collapsed && (
                  <Icon icon="mdi:chevron-left" width={22} height={22} />
                )}
                {hovered && collapsed && (
                  <Icon icon="mdi:chevron-right" width={22} height={22} />
                )}
              </AppIconButton>
            </AppTooltip>
          )}
        </div>

        <nav className="app-sidebar__nav custom-scrollbar">
          <ul className="app-sidebar__list">
            {items.map((page) => (
              <SidebarNavList
                key={page.title}
                href={page.href}
                icon={page.icon}
                title={page.title}
                collapsed={isDesktop && collapsed && !hovered}
                disabled={!canNavigate}
              />
            ))}
          </ul>
        </nav>
      </aside>
    </>
  );
};

export default Sidebar;
