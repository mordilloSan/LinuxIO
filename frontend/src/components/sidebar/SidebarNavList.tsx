import { Icon } from "@iconify/react";
import {
  ListItemButton,
  ListItemIcon,
  ListItemText,
  useTheme,
} from "@mui/material";
import { lighten } from "@mui/material/styles"; // use MUI's util
import React from "react";
import { NavLink, useLocation } from "react-router-dom";

interface SidebarNavListItemProps {
  href: string;
  title: string;
  icon?: React.ElementType | string;
  collapsed?: boolean;
}

const SidebarNavList: React.FC<SidebarNavListItemProps> = React.memo(
  ({ href, title, icon, collapsed = false }) => {
    const theme = useTheme();
    const { pathname } = useLocation();

    const isActive = pathname === href || pathname.startsWith(href + "/");

    // Trust the theme
    const primaryHex = theme.palette.primary.main;
    const contrast = theme.palette.primary.contrastText;
    const gradStart = lighten(primaryHex, 0.35);

    const renderIcon = () => {
      if (!icon) return null;
      if (typeof icon === "string")
        return <Icon icon={icon} width={24} height={24} />;
      const IconComponent = icon as React.ElementType;
      return <IconComponent />;
    };

    return (
      <ListItemButton
        component={NavLink}
        to={href}
        selected={isActive}
        sx={{
          margin: theme.spacing(1, 2),
          padding: theme.spacing(1.5, 3),
          borderRadius: "0 9999px 9999px 0",
          color: theme.sidebar.color,
          textTransform: "none",
          width: "auto",
          justifyContent: collapsed ? "center" : "flex-start",
          transition: "all 0.3s ease",
          "& svg": {
            color: theme.sidebar.color,
            width: 26,
            height: 26,
            transition: "margin 0.3s, color 0.3s",
            marginRight: collapsed ? 0 : theme.spacing(2),
          },
          "&.Mui-selected": {
            background: `linear-gradient(90deg, ${gradStart} 0%, ${primaryHex} 50%)`,
            color: contrast,
            "& svg": { color: contrast },
            "& .MuiListItemText-primary": {
              color: contrast,
              fontWeight: theme.typography.fontWeightMedium,
            },
          },
        }}
      >
        {icon && (
          <ListItemIcon
            sx={{
              minWidth: 0,
              justifyContent: "center",
              color: "inherit",
              transition: "margin 0.3s ease",
            }}
          >
            {renderIcon()}
          </ListItemIcon>
        )}
        <ListItemText
          primary={title}
          slotProps={{
            primary: {
              sx: {
                opacity: collapsed ? 0 : 1,
                transition: "opacity 0.3s ease",
                fontSize: theme.typography.body1.fontSize,
                whiteSpace: "nowrap",
              },
            },
          }}
        />
      </ListItemButton>
    );
  },
);

SidebarNavList.displayName = "SidebarNavList";

export default SidebarNavList;
