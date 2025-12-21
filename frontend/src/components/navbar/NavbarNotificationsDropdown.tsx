import {
  Badge,
  Box,
  Button,
  IconButton,
  List,
  ListItem,
  ListItemText,
  Popover,
  Tooltip,
  Typography,
  useTheme,
} from "@mui/material";
import Bell from "lucide-react/dist/esm/icons/bell";
import React, { useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast, useSonner, type ToastT } from "sonner";

const MAX_RECENT_TOASTS = 5;

function Notification({
  title,
  description,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
}) {
  return (
    <ListItem divider>
      <ListItemText
        primary={title}
        secondary={description}
        slotProps={{
          primary: {
            variant: "subtitle2",
            color: "text.primary",
          },
        }}
      />
    </ListItem>
  );
}

function NavbarNotificationsDropdown() {
  const theme = useTheme();
  const ref = useRef<HTMLButtonElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);
  const isOpen = Boolean(anchorEl);
  const { toasts } = useSonner();

  const handleOpen = () => setAnchorEl(ref.current);
  const handleClose = () => setAnchorEl(null);

  const recentToasts = useMemo(() => {
    const history = toast
      .getHistory()
      .filter((item): item is ToastT => !("dismiss" in item));

    return history.slice(-MAX_RECENT_TOASTS).reverse();
  }, [toasts]);

  const resolveToastNode = (
    node?: React.ReactNode | (() => React.ReactNode),
  ) => (typeof node === "function" ? node() : node);
  const recentToastCount = recentToasts.length;

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
            recentToasts.map((toastItem) => (
              <Notification
                key={toastItem.id}
                title={resolveToastNode(toastItem.title) || "Notification"}
                description={resolveToastNode(toastItem.description)}
              />
            ))
          )}
        </List>

        <Box p={1} display="flex" justifyContent="center">
          <Button size="small" component={Link} to="#">
            Show all notifications
          </Button>
        </Box>
      </Popover>
    </>
  );
}

export default NavbarNotificationsDropdown;
