import { Icon } from "@iconify/react";
import {
  ListItemButton,
  ListItemIcon,
  ListItemText,
  useTheme,
} from "@mui/material";
import { lighten } from "polished";
import React, { useContext } from "react";
import { NavLink, useLocation } from "react-router-dom";

import { ThemeContext } from "@/contexts/ThemeContext";

interface SidebarNavListItemProps {
  href: string;
  title: string;
  icon?: React.ElementType | string;
  collapsed?: boolean;
}

const SidebarNavListItem: React.FC<SidebarNavListItemProps> = ({
  href,
  title,
  icon,
  collapsed = false,
}) => {
  const theme = useTheme();
  const { pathname } = useLocation();
  const { primaryColor } = useContext(ThemeContext);
  const fallbackPrimary = "#3f5efb";

  const isActive = pathname === href || pathname.startsWith(href + "/");
  const activeColor = primaryColor || fallbackPrimary;

  const renderIcon = () => {
    if (!icon) return null;
    if (typeof icon === "string") {
      return <Icon icon={icon} width={24} height={24} />;
    }
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
        fontWeight: theme.typography.fontWeightRegular,
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
          background: `linear-gradient(90deg, ${lighten(
            0.25,
            activeColor,
          )} 0%, ${activeColor} 50%)`,
          color: "#fff",
          "& svg": {
            color: "#fff",
          },
          "& .MuiListItemText-primary": {
            color: "#fff",
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
              fontWeight: theme.typography.fontWeightRegular,
              whiteSpace: "nowrap",
            },
          },
        }}
      />
    </ListItemButton>
  );
};

export default SidebarNavListItem;
