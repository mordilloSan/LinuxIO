import { Icon } from "@iconify/react";
import React, { useState, useCallback } from "react";
import { toast } from "sonner";

import linuxio from "@/api/react-query";
import FrostedCard from "@/components/cards/RootCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
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
  const metaPillStyle: React.CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    borderRadius: 999,
    border: "1px solid var(--app-palette-divider)",
    background:
      "color-mix(in srgb, var(--app-palette-background-paper), transparent 10%)",
    padding: "4px 10px",
    fontSize: "0.75rem",
    lineHeight: 1.2,
    fontWeight: 600,
  };

  return (
    <>
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
            <h3
              style={{
                margin: 0,
                fontSize: "1rem",
                fontWeight: 600,
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
              }}
            >
              {module.title}
            </h3>
            <p
              style={{
                margin: "4px 0 0",
                color: "var(--app-palette-text-secondary)",
                fontSize: "0.875rem",
              }}
            >
              v{module.version}
            </p>
          </div>
        </div>

        {/* Description */}
        <p
          style={{
            margin: "0 0 8px",
            color: "var(--app-palette-text-secondary)",
            fontSize: "0.875rem",
            lineHeight: 1.45,
            overflow: "hidden",
            textOverflow: "ellipsis",
            display: "-webkit-box",
            WebkitLineClamp: 2,
            WebkitBoxOrient: "vertical",
            minHeight: "2.5em",
          }}
        >
          {module.description}
        </p>

        {/* Metadata Chips */}
        <div
          style={{
            display: "flex",
            gap: 4,
            marginBottom: 8,
            flexWrap: "wrap",
          }}
        >
          {isSystem ? (
            <span
              style={{
                ...metaPillStyle,
                borderColor:
                  "color-mix(in srgb, var(--app-palette-primary-main), transparent 55%)",
                background:
                  "color-mix(in srgb, var(--app-palette-primary-main), transparent 88%)",
              }}
            >
              System
            </span>
          ) : null}
          {isSymlink ? <span style={metaPillStyle}>Symlink</span> : null}
          <span style={metaPillStyle}>Route: {module.route}</span>
        </div>

        {/* Actions */}
        <div style={{ display: "flex", gap: 4, marginTop: "auto" }}>
          <AppTooltip title="View Details">
            <AppIconButton size="small" onClick={onViewDetails}>
              <Icon icon="mdi:information" width={20} height={20} />
            </AppIconButton>
          </AppTooltip>
          <AppTooltip title="Uninstall">
            <AppIconButton
              size="small"
              color="error"
              onClick={() => setUninstallDialogOpen(true)}
            >
              <Icon icon="mdi:delete" width={20} height={20} />
            </AppIconButton>
          </AppTooltip>
        </div>
      </FrostedCard>

      {/* Uninstall Confirmation Dialog */}
      <GeneralDialog
        open={uninstallDialogOpen}
        onClose={() => setUninstallDialogOpen(false)}
      >
        <AppDialogTitle>Uninstall Module</AppDialogTitle>
        <AppDialogContent>
          <p
            style={{
              margin: 0,
              lineHeight: 1.5,
            }}
          >
            Are you sure you want to uninstall <strong>{module.title}</strong>?
            This action cannot be undone.
          </p>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={() => setUninstallDialogOpen(false)}>
            Cancel
          </AppButton>
          <AppButton
            onClick={handleUninstall}
            color="error"
            variant="contained"
            disabled={uninstallMutation.isPending}
          >
            {uninstallMutation.isPending ? "Uninstalling..." : "Uninstall"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </>
  );
};

export default ModuleCard;
