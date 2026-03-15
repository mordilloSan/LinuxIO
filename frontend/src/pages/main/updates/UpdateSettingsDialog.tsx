import { Icon } from "@iconify/react";
import {
  Chip,
  DialogContent,
  DialogTitle,
  IconButton,
  Typography,
  useTheme,
} from "@mui/material";
import React from "react";

import UpdateSettings, { useUpdateSettingsState } from "./UpdateSettings";

import GeneralDialog from "@/components/dialog/GeneralDialog";

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
          <Typography variant="h6" sx={{ flexGrow: 1 }}>
            Automatic Updates
          </Typography>
          {settingsState.serverState ? (
            <Chip
              size="small"
              label={settingsState.serverState.backend}
              variant="outlined"
            />
          ) : null}
          <IconButton
            size="small"
            onClick={onClose}
            aria-label="Close update settings"
          >
            <Icon icon="mdi:close" width={18} height={18} />
          </IconButton>
        </div>
      </DialogTitle>

      <DialogContent sx={{ px: 3, py: 3 }}>
        <UpdateSettings disablePadding state={settingsState} />
      </DialogContent>
    </GeneralDialog>
  );
};

export default UpdateSettingsDialog;
