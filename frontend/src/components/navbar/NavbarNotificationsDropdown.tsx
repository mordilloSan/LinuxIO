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
import { Bell, Home, UserPlus, Server } from "lucide-react";
import React, { useRef, useState } from "react";
import { Link } from "react-router-dom";

function Notification({
  title,
  description,
}: {
  title: string;
  description: string;
  Icon: React.ElementType;
}) {
  return (
    <ListItem divider component={Link} to="#">
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
  const [isOpen, setOpen] = useState(false);

  const handleOpen = () => setOpen(true);
  const handleClose = () => setOpen(false);

  return (
    <>
      <Tooltip title="Notifications">
        <IconButton color="inherit" ref={ref} onClick={handleOpen} size="large">
          <Badge
            badgeContent={7}
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
        anchorEl={ref.current}
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
            7 New Notifications
          </Typography>
        </Box>

        <List disablePadding>
          <Notification
            title="Update complete"
            description="Restart server to complete update."
            Icon={Server}
          />
          <Notification
            title="New connection"
            description="Anna accepted your request."
            Icon={UserPlus}
          />
          <Notification
            title="Lorem ipsum"
            description="Aliquam ex eros, imperdiet vulputate hendrerit et"
            Icon={Bell}
          />
          <Notification
            title="New login"
            description="Login from 192.186.1.1."
            Icon={Home}
          />
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
