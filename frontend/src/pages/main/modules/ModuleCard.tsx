import { Icon } from "@iconify/react";
import DeleteIcon from "@mui/icons-material/Delete";
import InfoIcon from "@mui/icons-material/Info";
import {
  Box,
  Grid,
  Typography,
  Chip,
  IconButton,
  Tooltip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
} from "@mui/material";
import React, { useState, useCallback } from "react";
import { toast } from "sonner";

import linuxio from "@/api/react-query";
import FrostedCard from "@/components/cards/RootCard";
import type {
  ModuleInfo,
  ModuleDetailsInfo,
  UninstallResult,
} from "@/types/module";

interface ModuleCardProps {
  module: ModuleInfo;
  onViewDetails: () => void;
  onModuleChange: () => void;
}

const ModuleCard: React.FC<ModuleCardProps> = ({
  module,
  onViewDetails,
  onModuleChange,
}) => {
  const [uninstallDialogOpen, setUninstallDialogOpen] = useState(false);

  // Fetch detailed info to check if it's a system module
  const { data: moduleDetails } = linuxio.useCall<ModuleDetailsInfo>(
    "modules",
    "GetModuleDetails",
    [module.name],
    {
      enabled: true,
    },
  );

  // Uninstall mutation
  const uninstallMutation = linuxio.useMutate<UninstallResult, string>(
    "modules",
    "UninstallModule",
    {
      onSuccess: (result) => {
        toast.success(
          result.message || `Module ${module.title} uninstalled successfully`,
        );
        setUninstallDialogOpen(false);
        onModuleChange();
      },
    },
  );

  const handleUninstall = useCallback(() => {
    uninstallMutation.mutate(module.name);
  }, [module.name, uninstallMutation]);

  const isSystem = moduleDetails?.isSystem || false;
  const isSymlink = moduleDetails?.isSymlink || false;

  return (
    <>
      <Grid size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
        <FrostedCard
          sx={{
            p: 2,
            display: "flex",
            flexDirection: "column",
            height: "100%",
            transition: "transform 0.2s, box-shadow 0.2s",
            "&:hover": {
              transform: "translateY(-4px)",
              boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
            },
          }}
        >
          {/* Icon and Title */}
          <Box sx={{ display: "flex", alignItems: "flex-start", mb: 2 }}>
            <Box
              sx={{
                width: 48,
                height: 48,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                mr: 2,
              }}
            >
              {module.icon?.includes(":") ? (
                <Icon icon={module.icon} width={48} height={48} />
              ) : (
                <Icon icon="mdi:puzzle" width={48} height={48} />
              )}
            </Box>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <Typography variant="h6" noWrap>
                {module.title}
              </Typography>
              <Typography variant="body2" color="text.secondary">
                v{module.version}
              </Typography>
            </Box>
          </Box>

          {/* Description */}
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{
              mb: 2,
              overflow: "hidden",
              textOverflow: "ellipsis",
              display: "-webkit-box",
              WebkitLineClamp: 2,
              WebkitBoxOrient: "vertical",
              minHeight: "2.5em",
            }}
          >
            {module.description}
          </Typography>

          {/* Metadata Chips */}
          <Box sx={{ display: "flex", gap: 1, mb: 2, flexWrap: "wrap" }}>
            {isSystem && <Chip label="System" size="small" color="primary" />}
            {isSymlink && (
              <Chip label="Symlink" size="small" variant="outlined" />
            )}
            <Chip
              label={`Route: ${module.route}`}
              size="small"
              variant="outlined"
            />
          </Box>

          {/* Actions */}
          <Box sx={{ display: "flex", gap: 1, mt: "auto" }}>
            <Tooltip title="View Details">
              <IconButton size="small" onClick={onViewDetails}>
                <InfoIcon />
              </IconButton>
            </Tooltip>
            <Tooltip title="Uninstall">
              <IconButton
                size="small"
                color="error"
                onClick={() => setUninstallDialogOpen(true)}
              >
                <DeleteIcon />
              </IconButton>
            </Tooltip>
          </Box>
        </FrostedCard>
      </Grid>

      {/* Uninstall Confirmation Dialog */}
      <Dialog
        open={uninstallDialogOpen}
        onClose={() => setUninstallDialogOpen(false)}
      >
        <DialogTitle>Uninstall Module</DialogTitle>
        <DialogContent>
          <Typography>
            Are you sure you want to uninstall <strong>{module.title}</strong>?
            This action cannot be undone.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setUninstallDialogOpen(false)}>Cancel</Button>
          <Button
            onClick={handleUninstall}
            color="error"
            variant="contained"
            disabled={uninstallMutation.isPending}
          >
            {uninstallMutation.isPending ? "Uninstalling..." : "Uninstall"}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
};

export default ModuleCard;
