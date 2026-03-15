import { Icon } from "@iconify/react";
import { IconButton } from "@mui/material";
import React, { useCallback, useState } from "react";

import SettingsDialog from "./SettingsDialog";

import AppTooltip from "@/components/ui/AppTooltip";
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
      <AppTooltip title="Open settings">
        <IconButton
          color="inherit"
          aria-label="Open settings"
          onClick={handleOpen}
        >
          <Icon icon="mdi:cog" width={iconSize.md} height={iconSize.md} />
        </IconButton>
      </AppTooltip>

      <SettingsDialog open={open} onClose={handleClose} />
    </>
  );
};

export default React.memo(NavbarSettingsDialogTrigger);
