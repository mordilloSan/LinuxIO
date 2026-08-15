import { Icon } from "@iconify/react";
import { memo, useCallback, useState } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { iconSize } from "@/theme/constants";

import SettingsDialog from "./SettingsDialog";

interface NavbarSettingsDialogTriggerProps {
  /** false when the dock provides its own hover label for this control. */
  tooltip?: boolean;
}

const NavbarSettingsDialogTrigger = ({
  tooltip = true,
}: NavbarSettingsDialogTriggerProps) => {
  const [open, setOpen] = useState(false);

  const handleOpen = useCallback(() => {
    setOpen(true);
  }, []);

  const handleClose = useCallback(() => {
    setOpen(false);
  }, []);

  return (
    <>
      <AppTooltip title={tooltip ? "Open settings" : ""}>
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

export default memo(NavbarSettingsDialogTrigger);
