import { Icon } from "@iconify/react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import Chip from "@/components/ui/AppChip";
import { AppDialogContent, AppDialogTitle } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

import UpdateSettings, { useUpdateSettingsState } from "./UpdateSettings";

interface UpdateSettingsDialogProps {
  onClose: () => void;
  open: boolean;
}
const UpdateSettingsDialog = ({ open, onClose }: UpdateSettingsDialogProps) => {
  const theme = useAppTheme();
  const settingsState = useUpdateSettingsState(open);
  return (
    <GeneralDialog
      fullWidth
      maxWidth="md"
      onClose={onClose}
      open={open}
      paperStyle={{ borderRadius: 12 }}
    >
      <AppDialogTitle
        style={{
          backgroundColor: theme.palette.background.paper,
          borderBottom: `1px solid ${theme.palette.divider}`,
          padding: "10px 12px",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(1),
          }}
        >
          <div
            style={{
              alignItems: "center",
              background: theme.palette.action.hover,
              borderRadius: 9,
              color: theme.palette.primary.main,
              display: "inline-flex",
              flexShrink: 0,
              height: 36,
              justifyContent: "center",
              width: 36,
            }}
          >
            <Icon height={22} icon="mdi:update" width={22} />
          </div>
          <div
            style={{
              alignSelf: "stretch",
              display: "flex",
              flexDirection: "column",
              flexGrow: 1,
              justifyContent: "center",
              minWidth: 0,
            }}
          >
            <AppTypography fontWeight={600} style={{ lineHeight: 1.25 }} variant="subtitle1">
              Automatic Updates
            </AppTypography>
            <AppTypography color="text.secondary" style={{ lineHeight: 1.35 }} variant="caption">
              Linux package scheduling and installation policy
            </AppTypography>
          </div>
          {settingsState.serverState ? (
            <Chip label={settingsState.serverState.backend} size="small" variant="soft" />
          ) : null}
          <AppIconButton aria-label="Close update settings" onClick={onClose} size="small">
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
        </div>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          padding: 10,
        }}
      >
        <UpdateSettings disablePadding state={settingsState} />
      </AppDialogContent>
    </GeneralDialog>
  );
};
export default UpdateSettingsDialog;
