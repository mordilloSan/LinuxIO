import { Icon } from "@iconify/react";
import { Drawer, useTheme, List, IconButton, Tooltip } from "@mui/material";
import React, { useState, useCallback } from "react";

import SidebarNavList from "./SidebarNavList";
import LogoDisplay from "../logo/LogoDisplay";

import { collapsedDrawerWidth, drawerWidth } from "@/constants";
import { useLinuxIOUpdater } from "@/hooks/useLinuxIOUpdater";
import useSidebar from "@/hooks/useSidebar";
import { SidebarItemsType } from "@/types/sidebar";

export interface SidebarProps {
  items: SidebarItemsType[];
}

const Sidebar: React.FC<SidebarProps> = ({ items }) => {
  const theme = useTheme();
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

  return (
    <Drawer
      variant={isDesktop ? "permanent" : "temporary"}
      open={isDesktop ? true : mobileOpen}
      onClose={() => setMobileOpen(false)}
      ModalProps={{ keepMounted: true }}
      slotProps={{
        paper: {
          sx: {
            width: effectiveWidth,
            borderRight: 0,
            backgroundColor: theme.sidebar.background,
            transition: theme.transitions.create(["width"], {
              easing: theme.transitions.easing.sharp,
              duration: theme.transitions.duration.standard,
            }),
            overflowX: "hidden",
            "& > div": { borderRight: 0 },
          },
        },
      }}
      onMouseEnter={isDesktop ? handleMouseEnter : undefined}
      onMouseLeave={isDesktop ? handleMouseLeave : undefined}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          backgroundColor: theme.sidebar.header.background,
          minHeight: isDesktop ? 64 : 56,
          position: "relative",
          paddingLeft: theme.spacing(1.5),
          paddingRight: theme.spacing(1.5),
        }}
      >
        <LogoDisplay showText={showText} />

        {isDesktop && (!collapsed || (hovered && collapsed)) && (
          <Tooltip title={collapsed ? "Expand" : "Collapse"}>
            <IconButton
              onClick={toggleCollapse}
              size="small"
              sx={{
                position: "absolute",
                right: 4,
                top: "50%",
                transform: "translateY(-50%)",
              }}
              aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
              edge="end"
            >
              {!collapsed && <Icon icon="mdi:chevron-left" width={22} height={22} />}
              {hovered && collapsed && (
                <Icon icon="mdi:chevron-right" width={22} height={22} />
              )}
            </IconButton>
          </Tooltip>
        )}
      </div>

      <List disablePadding>
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
      </List>
    </Drawer>
  );
};

export default Sidebar;
