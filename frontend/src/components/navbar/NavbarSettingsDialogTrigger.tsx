import { Icon } from "@iconify/react";
import AppIconButton from "@/components/ui/AppIconButton";
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
        <AppIconButton
          color="inherit"
          aria-label="Open settings"
          onClick={handleOpen}
        >
          <Icon icon="mdi:cog" width={iconSize.md} height={iconSize.md} />
        </AppIconButton>
      </AppTooltip>

      <SettingsDialog open={open} onClose={handleClose} />
    </>
  );
};

export default React.memo(NavbarSettingsDialogTrigger);
