import { Icon } from "@iconify/react";
import { IconButton, Tooltip } from "@mui/material";
import React, { useCallback, useState } from "react";

import SettingsDialog from "./SettingsDialog";

import { iconSize } from "@/constants";

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
          aria-label="Open settings"
          onClick={handleOpen}
        >
          <Icon icon="mdi:cog" width={iconSize.md} height={iconSize.md} />
        </IconButton>
      </Tooltip>

      <SettingsDialog open={open} onClose={handleClose} />
    </>
  );
};

export default React.memo(NavbarSettingsDialogTrigger);
