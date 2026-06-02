import { Icon } from "@iconify/react";
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
import AppDivider from "@/components/ui/AppDivider";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppTypography from "@/components/ui/AppTypography";
export interface PruneOptions {
  buildCache: boolean;
  containers: boolean;
  images: boolean;
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
  isLoading?: boolean;
  onClose: () => void;
  onConfirm: (opts: PruneOptions) => void;
  open: boolean;
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
    <GeneralDialog fullWidth maxWidth="xs" onClose={handleClose} open={open}>
      <AppDialogTitle
        style={{
          display: "flex",
          alignItems: "center",
          gap: 4,
          borderBottom: "1px solid var(--mui-palette-divider)",
        }}
      >
        <Icon height={24} icon="mdi:broom" width={24} />
        <AppTypography variant="h6">Prune System</AppTypography>
      </AppDialogTitle>

      <AppDialogContent
        style={{
          paddingTop: 10,
          paddingBottom: 4,
        }}
      >
        <AppTypography
          color="text.secondary"
          style={{
            marginBottom: 8,
          }}
          variant="body2"
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
                disabled={isLoading}
                onChange={() => toggle("containers")}
              />
            }
            label="Stopped Containers"
          />
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.images}
                disabled={isLoading}
                onChange={() => toggle("images")}
              />
            }
            label="Unused Images (Not Used by Any Container)"
          />
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.buildCache}
                disabled={isLoading}
                onChange={() => toggle("buildCache")}
              />
            }
            label="Build Cache"
          />
          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.networks}
                disabled={isLoading}
                onChange={() => toggle("networks")}
              />
            }
            label="Unused Networks"
          />

          <AppDivider style={{ marginBlock: 2 }} />

          <AppFormControlLabel
            control={
              <AppCheckbox
                checked={opts.volumes}
                color="error"
                disabled={isLoading}
                onChange={() => toggle("volumes")}
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
                  color="error"
                  component="span"
                  fontWeight={600}
                  variant="caption"
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
        <AppButton color="inherit" disabled={isLoading} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          color={opts.volumes ? "error" : "warning"}
          disabled={isLoading || selectedCount === 0}
          onClick={() => onConfirm(opts)}
          startIcon={<Icon height={20} icon="mdi:broom" width={20} />}
          variant="contained"
        >
          {isLoading ? "Pruning..." : `Prune Selected (${selectedCount})`}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default PruneDialog;
