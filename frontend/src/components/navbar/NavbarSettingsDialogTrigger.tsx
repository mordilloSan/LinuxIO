import { Icon } from "@iconify/react";
import React, { useCallback, useState } from "react";

import SettingsDialog from "./SettingsDialog";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { iconSize } from "@/theme/constants";

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
          aria-label="Open settings"
          color="inherit"
          onClick={handleOpen}
        >
          <Icon height={iconSize.md} icon="mdi:cog" width={iconSize.md} />
        </AppIconButton>
      </AppTooltip>

      <SettingsDialog onClose={handleClose} open={open} />
    </>
  );
};

export default React.memo(NavbarSettingsDialogTrigger);
