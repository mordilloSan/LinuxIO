import { ChevronLeft, ChevronRight } from "@mui/icons-material";
import {
  Drawer,
  Box,
  useTheme,
  List,
  IconButton,
  Tooltip,
} from "@mui/material";
import React from "react";

import SidebarNavList from "./SidebarNavList";
import LogoDisplay from "../logo/LogoDisplay";

import { collapsedDrawerWidth, drawerWidth } from "@/constants";
import useSidebar from "@/hooks/useSidebar";
import { SidebarItemsType } from "@/types/sidebar";

export type SidebarProps = { items: SidebarItemsType[] };

const Sidebar: React.FC<SidebarProps> = ({ items }) => {
  const theme = useTheme();
  const {
    collapsed,
    hovered,
    setHovered,
    toggleCollapse,
    isDesktop,
    hoverEnabledRef,
    mobileOpen,
    setMobileOpen,
  } = useSidebar();

  const effectiveWidth = !isDesktop
    ? drawerWidth
    : collapsed && !hovered
      ? collapsedDrawerWidth
      : drawerWidth;

  const handleMouseEnter = () => {
    if (hoverEnabledRef.current) setHovered(true);
  };
  const handleMouseLeave = () => setHovered(false);

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
              {!collapsed && <ChevronLeft sx={{ width: 22, height: 22 }} />}
              {hovered && collapsed && (
                <ChevronRight sx={{ width: 22, height: 22 }} />
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
          />
        ))}
      </List>
    </Drawer>
  );
};

export default Sidebar;
