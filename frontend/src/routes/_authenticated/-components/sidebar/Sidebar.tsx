import { DndContext } from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { Icon } from "@iconify/react";
import { useCallback, useState, type CSSProperties } from "react";

import LogoDisplay from "@/components/logo/LogoDisplay";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useUpdateCanNavigate } from "@/hooks/useLinuxIOUpdater";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { collapsedDrawerWidth, drawerWidth } from "@/theme/constants";

import SidebarNavList from "./SidebarNavList";
import type { SidebarItem } from "./types";
import useSidebar from "./useSidebar";
import "@/components/reorder/reorder.css";
import "./sidebar.css";

const getNavItemId = (item: SidebarItem) => item.to;

export interface SidebarProps {
  items: SidebarItem[];
}

const Sidebar = ({ items }: SidebarProps) => {
  const { collapsed, toggleCollapse, isDesktop, mobileOpen, setMobileOpen } =
    useSidebar();
  const canNavigate = useUpdateCanNavigate();
  const surface = useReorderableSurface({
    disabled: !canNavigate,
    getId: getNavItemId,
    items,
    surface: "navigation.sidebar",
  });

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
    transition:
      "transform var(--app-transition-duration-fast) var(--app-easing-standard), width var(--app-transition-duration-fast) var(--app-easing-standard)",
  } as CSSProperties;

  return (
    <>
      {!isDesktop && mobileOpen && (
        <AppButton
          aria-label="Close navigation"
          className="app-sidebar-backdrop"
          onClick={() => setMobileOpen(false)}
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

        <nav className="app-sidebar__nav">
          <DndContext {...surface.dndContextProps}>
            <SortableContext
              items={surface.ids}
              strategy={verticalListSortingStrategy}
            >
              <ul
                className={
                  surface.editMode
                    ? "app-sidebar__list app-sidebar__list--reordering"
                    : "app-sidebar__list"
                }
              >
                {surface.items.map((page) => (
                  <SidebarNavList
                    {...page}
                    collapsed={isDesktop && collapsed && !hovered}
                    disabled={!canNavigate}
                    editMode={surface.editMode}
                    key={page.title}
                    pending={surface.pendingId === page.to}
                  />
                ))}
              </ul>
            </SortableContext>
          </DndContext>
        </nav>
      </aside>
    </>
  );
};

export default Sidebar;
