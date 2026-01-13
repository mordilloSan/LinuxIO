import ChevronLeftIcon from "@mui/icons-material/ChevronLeft";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import {
  Drawer,
  Box,
  useTheme,
  List,
  IconButton,
  Tooltip,
} from "@mui/material";
import React, { useState, useCallback } from "react";

import SidebarNavList from "./SidebarNavList";
import LogoDisplay from "../logo/LogoDisplay";

import { collapsedDrawerWidth, drawerWidth } from "@/constants";
import { useLinuxIOUpdater } from "@/hooks/useLinuxIOUpdater";
import useSidebar from "@/hooks/useSidebar";
import { SidebarItemsType } from "@/types/sidebar";

export type SidebarProps = { items: SidebarItemsType[] };

const Sidebar: React.FC<SidebarProps> = ({ items }) => {
  const theme = useTheme();
  const { collapsed, toggleCollapse, isDesktop, mobileOpen, setMobileOpen } =
    useSidebar();
  const { canNavigate } = useLinuxIOUpdater();

  // Local hover state - doesn't affect other components via context
  const [hovered, setHovered] = useState(false);

  const effectiveWidth = !isDesktop
    ? drawerWidth
    : collapsed && !hovered
      ? collapsedDrawerWidth
      : drawerWidth;

  // Only update hover state if sidebar is collapsed - no re-render needed when expanded
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
      ModalProps={{ keepMounted: true }} // smoother mobile perf
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
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          backgroundColor: theme.sidebar.header.background,
          minHeight: { xs: 56, sm: 64 },
          position: "relative",
          px: 1.5,
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
              {!collapsed && <ChevronLeftIcon sx={{ width: 22, height: 22 }} />}
              {hovered && collapsed && (
                <ChevronRightIcon sx={{ width: 22, height: 22 }} />
              )}
            </IconButton>
          </Tooltip>
        )}
      </Box>

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
