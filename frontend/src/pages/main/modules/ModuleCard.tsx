import { Icon } from "@iconify/react";
import {
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
import type { ModuleInfo } from "@/types/module";

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
  const { data: moduleDetails } = linuxio.modules.get_module_details.useQuery(
    module.name,
  );

  // Uninstall mutation
  const uninstallMutation = linuxio.modules.uninstall_module.useMutation({
    onSuccess: (result) => {
      toast.success(
        result.message || `Module ${module.title} uninstalled successfully`,
      );
      setUninstallDialogOpen(false);
      onModuleChange();
    },
  });

  const handleUninstall = useCallback(() => {
    uninstallMutation.mutate([module.name]);
  }, [module.name, uninstallMutation]);

  const isSystem = moduleDetails?.isSystem || false;
  const isSymlink = moduleDetails?.isSymlink || false;

  return (
    <>
      <Grid size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
        <FrostedCard
          hoverLift
          style={{
            padding: 8,
            display: "flex",
            flexDirection: "column",
            height: "100%",
          }}
        >
          {/* Icon and Title */}
          <div
            style={{
              display: "flex",
              alignItems: "flex-start",
              marginBottom: 8,
            }}
          >
            <div
              style={{
                width: 48,
                height: 48,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                marginRight: 8,
              }}
            >
              {module.icon?.includes(":") ? (
                <Icon icon={module.icon} width={48} height={48} />
              ) : (
                <Icon icon="mdi:puzzle" width={48} height={48} />
              )}
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <Typography variant="h6" noWrap>
                {module.title}
              </Typography>
              <Typography variant="body2" color="text.secondary">
                v{module.version}
              </Typography>
            </div>
          </div>

          {/* Description */}
          <Typography
            variant="body2"
            color="text.secondary"
            style={{
              marginBottom: 8,
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
          <div
            style={{
              display: "flex",
              gap: 4,
              marginBottom: 8,
              flexWrap: "wrap",
            }}
          >
            {isSystem && <Chip label="System" size="small" color="primary" />}
            {isSymlink && (
              <Chip label="Symlink" size="small" variant="outlined" />
            )}
            <Chip
              label={`Route: ${module.route}`}
              size="small"
              variant="outlined"
            />
          </div>

          {/* Actions */}
          <div style={{ display: "flex", gap: 4, marginTop: "auto" }}>
            <Tooltip title="View Details">
              <IconButton size="small" onClick={onViewDetails}>
                <Icon icon="mdi:information" width={20} height={20} />
              </IconButton>
            </Tooltip>
            <Tooltip title="Uninstall">
              <IconButton
                size="small"
                color="error"
                onClick={() => setUninstallDialogOpen(true)}
              >
                <Icon icon="mdi:delete" width={20} height={20} />
              </IconButton>
            </Tooltip>
          </div>
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
