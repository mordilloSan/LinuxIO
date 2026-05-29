import { Icon } from "@iconify/react";
import React from "react";

import type { ComposeProject } from "../../pages/main/docker/ComposeList";

import FrostedCard from "@/components/cards/FrostedCard";
import DockerIcon from "@/components/docker/DockerIcon";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSkeleton from "@/components/ui/AppSkeleton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import SkeletonText from "@/components/ui/SkeletonText";
import { getComposeStatusColor } from "@/constants/statusColors";
import { isLinuxIOManagedComposeProject } from "@/utils/dockerManaged";

const getStatusColor = (status: string) => {
  return getComposeStatusColor(status);
};

type ComposeStackCardProps =
  | { isPending: true }
  | {
      isPending?: false;
      project: ComposeProject;
      onStart: (projectName: string) => void;
      onStop: (projectName: string) => void;
      onRestart: (projectName: string) => void;
      onDelete: (project: ComposeProject) => void;
      onEdit?: (projectName: string, configPath: string) => void;
      onPreview?: (projectName: string, configPath: string) => void;
      isLoading?: boolean;
    };

const ComposeStackCard: React.FC<ComposeStackCardProps> = (props) => {
  if (props.isPending) {
    return (
      <FrostedCard
        hoverLift
        style={{
          padding: 8,
          display: "flex",
          flexDirection: "column",
          height: "100%",
          position: "relative",
        }}
      >
        <div style={{ position: "absolute", top: 12, right: 12 }}>
          <AppSkeleton
            height={22}
            style={{ borderRadius: 11 }}
            variant="text"
            width={56}
          />
        </div>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 6,
            paddingRight: 32,
          }}
        >
          <AppSkeleton height={36} variant="circular" width={36} />
          <SkeletonText variant="subtitle1" width="10ch" />
        </div>
        <div style={{ marginTop: 6, display: "flex", gap: 8 }}>
          <SkeletonText variant="body2" width="11ch" />
        </div>
        <AppDivider style={{ marginBlock: 12 }} />
        <div style={{ display: "flex", gap: 2 }}>
          <AppSkeleton height={28} variant="circular" width={28} />
          <AppSkeleton height={28} variant="circular" width={28} />
          <AppSkeleton height={28} variant="circular" width={28} />
          <AppSkeleton height={28} variant="circular" width={28} />
        </div>
      </FrostedCard>
    );
  }

  const {
    project,
    onStart,
    onStop,
    onRestart,
    onDelete,
    onEdit,
    onPreview,
    isLoading = false,
  } = props;
  const statusColor = getStatusColor(project.status);

  const totalContainers = Object.values(project.services).reduce(
    (acc, s) => acc + s.container_count,
    0,
  );
  const runningServices = Object.values(project.services).filter(
    (s) => s.state === "running",
  ).length;
  const totalServices = Object.keys(project.services).length;

  const isLinuxIOManaged = isLinuxIOManagedComposeProject(project.name);
  const isRunning =
    project.status === "running" || project.status === "partial";

  return (
    <FrostedCard
      hoverLift
      style={{
        padding: 8,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        position: "relative",
      }}
    >
      {/* Status chip top-right */}
      <div style={{ position: "absolute", top: 12, right: 12 }}>
        <Chip
          color={statusColor}
          label={project.status}
          size="small"
          sx={{
            textTransform: "capitalize",
            fontSize: "0.65rem",
            "& .MuiChip-label": { px: 1.5 },
          }}
          variant="soft"
        />
      </div>

      {/* Icon + Name */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          paddingRight: 32,
        }}
      >
        <DockerIcon alt={project.name} identifier={project.icon} size={36} />
        <AppTypography fontWeight={600} noWrap variant="subtitle1">
          {project.name}
        </AppTypography>
      </div>

      {/* Stats */}
      <div style={{ marginTop: 6, display: "flex", gap: 8 }}>
        <AppTypography color="text.secondary" variant="body2">
          {totalServices > 0
            ? `${runningServices}/${totalServices} services`
            : "No services"}
        </AppTypography>
        {totalContainers > 0 && (
          <AppTypography color="text.secondary" variant="body2">
            {totalContainers} container{totalContainers !== 1 ? "s" : ""}
          </AppTypography>
        )}
      </div>

      <AppDivider style={{ marginBlock: 12 }} />

      {/* Actions */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginTop: "auto",
        }}
      >
        {isLinuxIOManaged ? (
          <AppTooltip title="View compose file">
            <Chip
              className="chip-interactive"
              label="Managed by LinuxIO"
              onClick={
                onPreview && project.config_files.length > 0
                  ? () => onPreview(project.name, project.config_files[0])
                  : undefined
              }
              size="small"
              style={{
                fontSize: "0.68rem",
                cursor:
                  onPreview && project.config_files.length > 0
                    ? "pointer"
                    : "default",
              }}
              variant="soft"
            />
          </AppTooltip>
        ) : (
          <>
            <div style={{ display: "flex", gap: 2 }}>
              {onEdit && project.config_files.length > 0 && (
                <AppTooltip title="Edit">
                  <AppIconButton
                    disabled={isLoading}
                    onClick={() =>
                      onEdit(project.name, project.config_files[0])
                    }
                    size="small"
                  >
                    <Icon height={20} icon="mdi:pencil" width={20} />
                  </AppIconButton>
                </AppTooltip>
              )}
              {isRunning ? (
                <>
                  <AppTooltip title="Restart">
                    <AppIconButton
                      disabled={isLoading}
                      onClick={() => onRestart(project.name)}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:restart" width={20} />
                    </AppIconButton>
                  </AppTooltip>
                  <AppTooltip title="Stop">
                    <AppIconButton
                      disabled={isLoading}
                      onClick={() => onStop(project.name)}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:stop-circle" width={20} />
                    </AppIconButton>
                  </AppTooltip>
                </>
              ) : (
                <AppTooltip title="Start">
                  <AppIconButton
                    disabled={isLoading}
                    onClick={() => onStart(project.name)}
                    size="small"
                  >
                    <Icon height={20} icon="mdi:play" width={20} />
                  </AppIconButton>
                </AppTooltip>
              )}
              <AppTooltip title="Delete">
                <AppIconButton
                  disabled={isLoading}
                  onClick={() => onDelete(project)}
                  size="small"
                >
                  <Icon height={20} icon="mdi:delete" width={20} />
                </AppIconButton>
              </AppTooltip>
            </div>
          </>
        )}
      </div>
    </FrostedCard>
  );
};

export default ComposeStackCard;
