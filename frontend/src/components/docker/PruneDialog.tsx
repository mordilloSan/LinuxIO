import { Icon } from "@iconify/react";
import { Divider } from "@mui/material";
import React, { useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppTypography from "@/components/ui/AppTypography";
export interface PruneOptions {
  containers: boolean;
  images: boolean;
  buildCache: boolean;
  networks: boolean;
  volumes: boolean;
}
const defaultOptions: PruneOptions = {
  containers: true,
  images: true,
  buildCache: false,
  networks: true,
  volumes: false,
};
interface PruneDialogProps {
  open: boolean;
  onClose: () => void;
  onConfirm: (opts: PruneOptions) => void;
  isLoading?: boolean;
}
const PruneDialog: React.FC<PruneDialogProps> = ({
  open,
  onClose,
  onConfirm,
  isLoading = false,
}) => {
  const [opts, setOpts] = useState<PruneOptions>(defaultOptions);
  const toggle = (key: keyof PruneOptions) =>
    setOpts((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
  const selectedCount = Object.values(opts).filter(Boolean).length;
  const handleClose = () => {
    if (!isLoading) {
      setOpts(defaultOptions);
      onClose();
    }
  };
  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="xs" fullWidth>
      <AppDialogTitle
        style={{
          display: "flex",
          alignItems: "center",
          gap: 4,
          borderBottom: "1px solid var(--mui-palette-divider)",
        }}
      >
        <Icon icon="mdi:broom" width={24} height={24} />
        <AppTypography variant="h6">Prune System</AppTypography>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          paddingTop: 10,
          paddingBottom: 4,
        }}
      >
        <AppTypography
          variant="body2"
          color="text.secondary"
          style={{
            marginBottom: 8,
          }}
        >
          Select which unused Docker resources to remove:
        </AppTypography>

        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 2,
          }}
        >
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.containers}
                onChange={() => toggle("containers")}
                disabled={isLoading}
              />
            }
            label="Stopped Containers"
          />
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.images}
                onChange={() => toggle("images")}
                disabled={isLoading}
              />
            }
            label="Unused Images (Not Used by Any Container)"
          />
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.buildCache}
                onChange={() => toggle("buildCache")}
                disabled={isLoading}
              />
            }
            label="Build Cache"
          />
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.networks}
                onChange={() => toggle("networks")}
                disabled={isLoading}
              />
            }
            label="Unused Networks"
          />

          <Divider
            sx={{
              my: 0.5,
            }}
          />

          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.volumes}
                onChange={() => toggle("volumes")}
                disabled={isLoading}
                color="error"
              />
            }
            label={
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 2,
                }}
              >
                <span>Unused Volumes</span>
                <AppTypography
                  component="span"
                  variant="caption"
                  color="error"
                  fontWeight={600}
                >
                  (Potentially Destructive!)
                </AppTypography>
              </div>
            }
          />
        </div>

        {opts.volumes && (
          <AppAlert
            severity="warning"
            style={{
              marginTop: 8,
            }}
          >
            <AppTypography variant="body2">
              <strong>Warning:</strong> Removing unused volumes will permanently
              delete data that is not attached to any container. This cannot be
              undone.
            </AppTypography>
          </AppAlert>
        )}
      </AppDialogContent>

      <AppDialogActions
        style={{
          padding: 8,
          borderTop: "1px solid var(--mui-palette-divider)",
        }}
      >
        <AppButton onClick={handleClose} disabled={isLoading} color="inherit">
          Cancel
        </AppButton>
        <AppButton
          onClick={() => onConfirm(opts)}
          disabled={isLoading || selectedCount === 0}
          variant="contained"
          color={opts.volumes ? "error" : "warning"}
          startIcon={<Icon icon="mdi:broom" width={20} height={20} />}
        >
          {isLoading ? "Pruning..." : `Prune Selected (${selectedCount})`}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default PruneDialog;
