import { IconButton, Tooltip } from "@mui/material";
import Settings from "lucide-react/dist/esm/icons/settings";
import React, { useCallback, useState } from "react";

import SettingsDialog from "./SettingsDialog";

const NavbarSettingsDialogTrigger: React.FC = () => {
  const [open, setOpen] = useState(false);

  const handleOpen = useCallback(() => {
    setOpen(true);
  }, []);

  const handleClose = useCallback(() => {
    setOpen(false);
  }, []);

  return (
    <>
      <Tooltip title="Open settings">
        <IconButton
          color="inherit"
          size="large"
          aria-label="Open settings"
          onClick={handleOpen}
        >
          <Settings size={18} />
        </IconButton>
      </Tooltip>

      <SettingsDialog open={open} onClose={handleClose} />
    </>
  );
};

export default React.memo(NavbarSettingsDialogTrigger);
