import { Icon } from "@iconify/react";
import React from "react";

import type { ComposeProject } from "../../pages/main/docker/ComposeList";

import FrostedCard from "@/components/cards/RootCard";
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
            variant="text"
            width={56}
            height={22}
            style={{ borderRadius: 11 }}
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
          <AppSkeleton variant="circular" width={36} height={36} />
          <SkeletonText variant="subtitle1" width="10ch" />
        </div>
        <div style={{ marginTop: 6, display: "flex", gap: 8 }}>
          <SkeletonText variant="body2" width="11ch" />
        </div>
        <AppDivider style={{ marginBlock: 12 }} />
        <div style={{ display: "flex", gap: 2 }}>
          <AppSkeleton variant="circular" width={28} height={28} />
          <AppSkeleton variant="circular" width={28} height={28} />
          <AppSkeleton variant="circular" width={28} height={28} />
          <AppSkeleton variant="circular" width={28} height={28} />
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
          label={project.status}
          size="small"
          color={statusColor}
          variant="soft"
          sx={{
            textTransform: "capitalize",
            fontSize: "0.65rem",
            "& .MuiChip-label": { px: 1.5 },
          }}
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
        <DockerIcon identifier={project.icon} size={36} alt={project.name} />
        <AppTypography variant="subtitle1" fontWeight={600} noWrap>
          {project.name}
        </AppTypography>
      </div>

      {/* Stats */}
      <div style={{ marginTop: 6, display: "flex", gap: 8 }}>
        <AppTypography variant="body2" color="text.secondary">
          {totalServices > 0
            ? `${runningServices}/${totalServices} services`
            : "No services"}
        </AppTypography>
        {totalContainers > 0 && (
          <AppTypography variant="body2" color="text.secondary">
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
              label="Managed by LinuxIO"
              size="small"
              variant="soft"
              onClick={
                onPreview && project.config_files.length > 0
                  ? () => onPreview(project.name, project.config_files[0])
                  : undefined
              }
              className="chip-interactive"
              style={{
                fontSize: "0.68rem",
                cursor:
                  onPreview && project.config_files.length > 0
                    ? "pointer"
                    : "default",
              }}
            />
          </AppTooltip>
        ) : (
          <>
            <div style={{ display: "flex", gap: 2 }}>
              {onEdit && project.config_files.length > 0 && (
                <AppTooltip title="Edit">
                  <AppIconButton
                    size="small"
                    onClick={() =>
                      onEdit(project.name, project.config_files[0])
                    }
                    disabled={isLoading}
                  >
                    <Icon icon="mdi:pencil" width={20} height={20} />
                  </AppIconButton>
                </AppTooltip>
              )}
              {isRunning ? (
                <>
                  <AppTooltip title="Restart">
                    <AppIconButton
                      size="small"
                      onClick={() => onRestart(project.name)}
                      disabled={isLoading}
                    >
                      <Icon icon="mdi:restart" width={20} height={20} />
                    </AppIconButton>
                  </AppTooltip>
                  <AppTooltip title="Stop">
                    <AppIconButton
                      size="small"
                      onClick={() => onStop(project.name)}
                      disabled={isLoading}
                    >
                      <Icon icon="mdi:stop-circle" width={20} height={20} />
                    </AppIconButton>
                  </AppTooltip>
                </>
              ) : (
                <AppTooltip title="Start">
                  <AppIconButton
                    size="small"
                    onClick={() => onStart(project.name)}
                    disabled={isLoading}
                  >
                    <Icon icon="mdi:play" width={20} height={20} />
                  </AppIconButton>
                </AppTooltip>
              )}
              <AppTooltip title="Delete">
                <AppIconButton
                  size="small"
                  onClick={() => onDelete(project)}
                  disabled={isLoading}
                >
                  <Icon icon="mdi:delete" width={20} height={20} />
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
