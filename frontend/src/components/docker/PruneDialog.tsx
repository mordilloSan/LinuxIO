import CleaningServicesIcon from "@mui/icons-material/CleaningServices";
import WarningAmberIcon from "@mui/icons-material/WarningAmber";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControlLabel,
  Typography,
} from "@mui/material";
import React, { useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";

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
    setOpts((prev) => ({ ...prev, [key]: !prev[key] }));

  const selectedCount = Object.values(opts).filter(Boolean).length;

  const handleClose = () => {
    if (!isLoading) {
      setOpts(defaultOptions);
      onClose();
    }
  };

  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="xs" fullWidth>
      <DialogTitle
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          borderBottom: "1px solid",
          borderColor: "divider",
        }}
      >
        <CleaningServicesIcon color="error" />
        <Typography variant="h6">Prune System</Typography>
      </DialogTitle>

      <DialogContent sx={{ pt: 2.5, pb: 1 }}>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Select which unused Docker resources to remove:
        </Typography>

        <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
          <FormControlLabel
            control={
              <Checkbox
                checked={opts.containers}
                onChange={() => toggle("containers")}
                disabled={isLoading}
              />
            }
            label="Stopped Containers"
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={opts.images}
                onChange={() => toggle("images")}
                disabled={isLoading}
              />
            }
            label="Unused Images (Not Used by Any Container)"
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={opts.buildCache}
                onChange={() => toggle("buildCache")}
                disabled={isLoading}
              />
            }
            label="Build Cache"
          />
          <FormControlLabel
            control={
              <Checkbox
                checked={opts.networks}
                onChange={() => toggle("networks")}
                disabled={isLoading}
              />
            }
            label="Unused Networks"
          />

          <Divider sx={{ my: 0.5 }} />

          <FormControlLabel
            control={
              <Checkbox
                checked={opts.volumes}
                onChange={() => toggle("volumes")}
                disabled={isLoading}
                color="error"
              />
            }
            label={
              <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
                <span>Unused Volumes</span>
                <Typography
                  component="span"
                  variant="caption"
                  color="error"
                  fontWeight={600}
                >
                  (Potentially Destructive!)
                </Typography>
              </Box>
            }
          />
        </Box>

        {opts.volumes && (
          <Alert severity="warning" icon={<WarningAmberIcon />} sx={{ mt: 2 }}>
            <Typography variant="body2">
              <strong>Warning:</strong> Removing unused volumes will permanently
              delete data that is not attached to any container. This cannot be
              undone.
            </Typography>
          </Alert>
        )}
      </DialogContent>

      <DialogActions
        sx={{ p: 2, borderTop: "1px solid", borderColor: "divider" }}
      >
        <Button onClick={handleClose} disabled={isLoading} color="inherit">
          Cancel
        </Button>
        <Button
          onClick={() => onConfirm(opts)}
          disabled={isLoading || selectedCount === 0}
          variant="contained"
          color={opts.volumes ? "error" : "warning"}
          startIcon={<CleaningServicesIcon />}
        >
          {isLoading ? "Pruning..." : `Prune Selected (${selectedCount})`}
        </Button>
      </DialogActions>
    </GeneralDialog>
  );
};

export default PruneDialog;
