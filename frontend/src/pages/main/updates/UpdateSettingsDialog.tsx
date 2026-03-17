import { Icon } from "@iconify/react";
import {
  DialogContent,
  DialogTitle,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import AppIconButton from "@/components/ui/AppIconButton";
import React from "react";

import UpdateSettings, { useUpdateSettingsState } from "./UpdateSettings";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
interface UpdateSettingsDialogProps {
  open: boolean;
  onClose: () => void;
}
const UpdateSettingsDialog: React.FC<UpdateSettingsDialogProps> = ({
  open,
  onClose,
}) => {
  const theme = useTheme();
  const settingsState = useUpdateSettingsState(open);
  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle
        sx={{
          backgroundColor: theme.palette.background.paper,
          borderBottom: `1px solid ${theme.palette.divider}`,
          py: 1.5,
          px: 2,
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
      </DialogTitle>

      <DialogContent
        sx={{
          px: 3,
          py: 3,
        }}
      >
        <UpdateSettings disablePadding state={settingsState} />
      </DialogContent>
    </GeneralDialog>
  );
};
export default UpdateSettingsDialog;
