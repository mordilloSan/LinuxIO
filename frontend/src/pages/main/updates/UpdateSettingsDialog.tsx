import { Icon } from "@iconify/react";
import React from "react";

import UpdateSettings, { useUpdateSettingsState } from "./UpdateSettings";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import Chip from "@/components/ui/AppChip";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
interface UpdateSettingsDialogProps {
  open: boolean;
  onClose: () => void;
}
const UpdateSettingsDialog: React.FC<UpdateSettingsDialogProps> = ({
  open,
  onClose,
}) => {
  const theme = useAppTheme();
  const settingsState = useUpdateSettingsState(open);
  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <AppDialogTitle
        style={{
          backgroundColor: theme.palette.background.paper,
          borderBottom: `1px solid ${theme.palette.divider}`,
          paddingTop: 6,
          paddingBottom: 6,
          paddingLeft: 8,
          paddingRight: 8,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(1),
          }}
        >
          <AppTypography
            variant="h6"
            style={{
              flexGrow: 1,
            }}
          >
            Automatic Updates
          </AppTypography>
          {settingsState.serverState ? (
            <Chip
              size="small"
              label={settingsState.serverState.backend}
              variant="soft"
            />
          ) : null}
          <AppIconButton
            size="small"
            onClick={onClose}
            aria-label="Close update settings"
          >
            <Icon icon="mdi:close" width={18} height={18} />
          </AppIconButton>
        </div>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          paddingLeft: 12,
          paddingRight: 12,
          paddingTop: 12,
          paddingBottom: 12,
        }}
      >
        <UpdateSettings disablePadding state={settingsState} />
      </AppDialogContent>
    </GeneralDialog>
  );
};
export default UpdateSettingsDialog;
