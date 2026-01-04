import {
  Badge,
  Box,
  Button,
  IconButton,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Popover,
  Tooltip,
  Typography,
  useTheme,
} from "@mui/material";
import AlertTriangle from "lucide-react/dist/esm/icons/alert-triangle";
import Bell from "lucide-react/dist/esm/icons/bell";
import CheckCircle from "lucide-react/dist/esm/icons/check-circle";
import Info from "lucide-react/dist/esm/icons/info";
import Loader2 from "lucide-react/dist/esm/icons/loader-2";
import XCircle from "lucide-react/dist/esm/icons/x-circle";
import React, { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";

import { type ToastHistoryItem } from "@/contexts/ToastContext";
import { useClearToastHistory, useToastHistory } from "@/hooks/useToastHistory";

const MAX_RECENT_TOASTS = 5;

function Notification({
  title,
  description,
  timeLabel,
  icon,
  iconColor,
  link,
  onNavigate,
}: {
  title: string;
  description?: string;
  timeLabel?: string;
  icon: React.ReactNode;
  iconColor: string;
  link?: { href: string; label?: string };
  onNavigate?: () => void;
}) {
  const primaryText = description ? `${title} — ${description}` : title;
  const secondaryContent = (
    <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
      <Typography variant="caption" color="text.secondary">
        {timeLabel || ""}
      </Typography>
      {link ? (
        <Button
          size="small"
          component={Link}
          to={link.href}
          onClick={onNavigate}
          sx={{ ml: "auto", minWidth: "auto", p: 0, lineHeight: 1.2 }}
        >
          {link.label || "Open"}
        </Button>
      ) : null}
    </Box>
  );

  return (
    <ListItem divider sx={{ alignItems: "center" }}>
      <ListItemIcon sx={{ minWidth: 36, color: iconColor }}>
        {icon}
      </ListItemIcon>
      <ListItemText
        disableTypography
        primary={
          <Typography variant="subtitle2" color="text.primary">
            {primaryText}
          </Typography>
        }
        secondary={secondaryContent}
      />
    </ListItem>
  );
}

function NavbarNotificationsDropdown() {
  const theme = useTheme();
  const ref = useRef<HTMLButtonElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);
  const [now, setNow] = useState(0);
  const isOpen = Boolean(anchorEl);
  const recentToasts = useToastHistory(MAX_RECENT_TOASTS);
  const clearToastHistory = useClearToastHistory();

  const handleOpen = () => {
    setNow(Date.now());
    setAnchorEl(ref.current);
  };
  const handleClose = () => setAnchorEl(null);

  const recentToastCount = recentToasts.length;
  const iconSize = 18;

  useEffect(() => {
    if (!isOpen) return;
    const intervalId = window.setInterval(() => {
      setNow(Date.now());
    }, 60_000);
    return () => {
      window.clearInterval(intervalId);
    };
  }, [isOpen]);

  const formatTimeAgo = (timestamp: number) => {
    if (!now) return "";
    const diff = Math.max(0, now - timestamp);
    if (diff < 60_000) return "just now";
    const minutes = Math.floor(diff / 60_000);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 7) return `${days}d ago`;
    const weeks = Math.floor(days / 7);
    if (weeks < 4) return `${weeks}w ago`;
    const months = Math.floor(days / 30);
    if (months < 12) return `${months}mo ago`;
    const years = Math.floor(days / 365);
    return `${years}y ago`;
  };

  const getToastVisuals = (type?: ToastHistoryItem["type"]) => {
    switch (type) {
      case "success":
        return {
          icon: <CheckCircle size={iconSize} />,
          color: theme.palette.success.main,
        };
      case "error":
        return {
          icon: <XCircle size={iconSize} />,
          color: theme.palette.error.main,
        };
      case "warning":
        return {
          icon: <AlertTriangle size={iconSize} />,
          color: theme.palette.warning.main,
        };
      case "info":
        return {
          icon: <Info size={iconSize} />,
          color: theme.palette.info.main,
        };
      case "loading":
        return {
          icon: <Loader2 size={iconSize} />,
          color: theme.palette.text.secondary,
        };
      default:
        return {
          icon: <Bell size={iconSize} />,
          color: theme.palette.text.secondary,
        };
    }
  };

  return (
    <>
      <Tooltip title="Notifications">
        <IconButton color="inherit" ref={ref} onClick={handleOpen} size="large">
          <Badge
            badgeContent={recentToastCount}
            sx={{
              "& .MuiBadge-badge": {
                backgroundColor: theme.header.indicator.background,
                color: theme.palette.common.white,
              },
            }}
          >
            <Bell />
          </Badge>
        </IconButton>
      </Tooltip>

      <Popover
        anchorOrigin={{ vertical: "bottom", horizontal: "center" }}
        anchorEl={anchorEl}
        onClose={handleClose}
        open={isOpen}
        slotProps={{
          paper: {
            sx: {
              width: 300,
              border: `1px solid ${theme.palette.divider}`,
              boxShadow: theme.shadows[1],
            },
          },
        }}
      >
        <Box
          sx={{
            textAlign: "center",
            borderBottom: `1px solid ${theme.palette.divider}`,
            p: 2,
          }}
        >
          <Typography variant="subtitle2" color="textPrimary">
            {recentToastCount === 0
              ? "No notifications yet"
              : `Last ${recentToastCount} notification${
                  recentToastCount === 1 ? "" : "s"
                }`}
          </Typography>
        </Box>

        <List disablePadding>
          {recentToastCount === 0 ? (
            <ListItem>
              <ListItemText primary="You're all caught up." />
            </ListItem>
          ) : (
            recentToasts.map((toastItem) => {
              const visuals = getToastVisuals(toastItem.type);
              return (
                <Notification
                  key={toastItem.id}
                  title={toastItem.title}
                  description={toastItem.description}
                  timeLabel={formatTimeAgo(toastItem.createdAt)}
                  icon={visuals.icon}
                  iconColor={visuals.color}
                  link={
                    toastItem.meta?.href
                      ? {
                          href: toastItem.meta.href,
                          label: toastItem.meta.label,
                        }
                      : undefined
                  }
                  onNavigate={handleClose}
                />
              );
            })
          )}
        </List>

        <Box p={1} display="flex" justifyContent="center" gap={1}>
          <Button size="small" component={Link} to="#">
            Show all notifications
          </Button>
          <Button
            size="small"
            onClick={clearToastHistory}
            disabled={recentToastCount === 0}
          >
            Clear
          </Button>
        </Box>
      </Popover>
    </>
  );
}

export default NavbarNotificationsDropdown;
