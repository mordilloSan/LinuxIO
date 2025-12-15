import {
  Divider,
  Tooltip,
  Menu,
  MenuItem,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  Button,
} from "@mui/material";
import LucidePower from "lucide-react/dist/esm/icons/power";
import React, { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import useAuth from "@/hooks/useAuth";
import axios from "@/utils/axios";

function NavbarUserDropdown() {
  const ref = useRef(null);
  const navigate = useNavigate();
  const { user, signOut } = useAuth();

  const [anchorMenu, setAnchorMenu] = useState<null | HTMLElement>(null);
  const [confirm, setConfirm] = useState<"reboot" | "poweroff" | null>(null);

  const toggleMenu = (event: React.SyntheticEvent<HTMLElement>) => {
    setAnchorMenu(event.currentTarget);
  };

  const closeMenu = () => setAnchorMenu(null);
  const closeConfirm = () => setConfirm(null);

  const handleSignOut = async () => {
    await signOut();
    navigate("/sign-in");
  };

  const handleConfirmedAction = async () => {
    closeMenu();
    try {
      if (confirm === "reboot") {
        await axios.post("/power/reboot");
      } else if (confirm === "poweroff") {
        await axios.post("/power/shutdown");
      }
    } catch (err) {
      console.error(`Failed to ${confirm} system:`, err);
    } finally {
      closeConfirm();
    }
  };

  return (
    <>
      <Tooltip title="Account">
        <IconButton color="inherit" ref={ref} onClick={toggleMenu} size="large">
          <LucidePower />
        </IconButton>
      </Tooltip>

      <Menu
        id="menu-appbar"
        anchorEl={anchorMenu}
        open={Boolean(anchorMenu)}
        onClose={closeMenu}
      >
        {user?.name && (
          <MenuItem disabled style={{ opacity: 0.7, fontWeight: 600 }}>
            Signed in as {user.name}
          </MenuItem>
        )}
        <Divider />
        <MenuItem onClick={() => setConfirm("reboot")}>Reboot</MenuItem>
        <MenuItem onClick={() => setConfirm("poweroff")}>Power Down</MenuItem>
        <Divider />
        <MenuItem onClick={handleSignOut}>Sign out</MenuItem>
      </Menu>

      <Dialog open={confirm !== null} onClose={closeConfirm}>
        <DialogTitle>
          {confirm === "reboot" ? "Confirm Reboot" : "Confirm Power Down"}
        </DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to{" "}
            {confirm === "reboot" ? "reboot" : "power off"} the server? This
            action will terminate all services and disconnect users.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={closeConfirm}>Cancel</Button>
          <Button
            onClick={handleConfirmedAction}
            color="error"
            variant="contained"
            autoFocus
          >
            {confirm === "reboot" ? "Reboot" : "Power Down"}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}

export default NavbarUserDropdown;
