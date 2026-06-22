import React from "react";

import type { ComposeProject } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import DockerIcon from "@/components/docker/DockerIcon";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppSkeleton from "@/components/ui/AppSkeleton";
import AppTypography from "@/components/ui/AppTypography";
import { getComposeStatusColor } from "@/constants/statusColors";

const getStatusColor = (status: string) => {
  return getComposeStatusColor(status);
};

const DOCKER_TOAST_META = { href: "/docker", label: "Open Docker" };

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
  // The card chrome (shell, layout, divider) is rendered unconditionally;
  // only the data-bearing leaves fall back to skeletons while pending.
  const loaded = props.isPending ? null : props;
  const project = loaded?.project;
  const onStart = loaded?.onStart;
  const onStop = loaded?.onStop;
  const onRestart = loaded?.onRestart;
  const onDelete = loaded?.onDelete;
  const onEdit = loaded?.onEdit;
  const isLoading = loaded?.isLoading ?? false;

  const totalContainers = project
    ? Object.values(project.services).reduce(
        (acc, s) => acc + s.container_count,
        0,
      )
    : 0;
  const runningServices = project
    ? Object.values(project.services).filter((s) => s.state === "running").length
    : 0;
  const totalServices = project ? Object.keys(project.services).length : 0;
  const isRunning =
    project?.status === "running" || project?.status === "partial";

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
        {project ? (
          <Chip
            color={getStatusColor(project.status)}
            label={project.status}
            size="small"
            style={{
              textTransform: "capitalize",
              fontSize: "0.65rem",
            }}
            labelStyle={{ paddingInline: 6 }}
            variant="soft"
          />
        ) : (
          <AppSkeleton
            height={22}
            style={{ borderRadius: 11 }}
            variant="text"
            width={56}
          />
        )}
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
        {project ? (
          <DockerIcon alt={project.name} identifier={project.icon} size={36} />
        ) : (
          <AppSkeleton height={36} variant="circular" width={36} />
        )}
        {project ? (
          <AppTypography
            copyText={project.name}
            fontWeight={600}
            noWrap
            title={project.name}
            toastMeta={DOCKER_TOAST_META}
            variant="subtitle1"
          >
            {project.name}
          </AppTypography>
        ) : (
          <AppSkeleton textVariant="subtitle1" width="10ch" />
        )}
      </div>

      {/* Stats */}
      <div style={{ marginTop: 6, display: "flex", gap: 8 }}>
        {project ? (
          <>
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
            {project.update_available && (
              <Chip
                color="warning"
                label="Update available"
                size="small"
                style={{ fontSize: "0.68rem" }}
                variant="soft"
              />
            )}
          </>
        ) : (
          <AppSkeleton textVariant="body2" width="11ch" />
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
        <div style={{ display: "flex", gap: 2 }}>
          {project ? (
            <>
              {onEdit && project.config_files.length > 0 && (
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:pencil"
                  iconSize={20}
                  label="Edit"
                  onClick={() => onEdit(project.name, project.config_files[0])}
                />
              )}
              {isRunning ? (
                <>
                  <AppActionIconButton
                    disabled={isLoading}
                    icon="mdi:restart"
                    iconSize={20}
                    label="Restart"
                    onClick={() => onRestart?.(project.name)}
                  />
                  <AppActionIconButton
                    disabled={isLoading}
                    icon="mdi:stop-circle"
                    iconSize={20}
                    label="Stop"
                    onClick={() => onStop?.(project.name)}
                  />
                </>
              ) : (
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:play"
                  iconSize={20}
                  label="Start"
                  onClick={() => onStart?.(project.name)}
                />
              )}
              <AppActionIconButton
                disabled={isLoading}
                icon="mdi:delete"
                iconSize={20}
                label="Delete"
                onClick={() => onDelete?.(project)}
              />
            </>
          ) : (
            <>
              <AppSkeleton height={28} variant="circular" width={28} />
              <AppSkeleton height={28} variant="circular" width={28} />
              <AppSkeleton height={28} variant="circular" width={28} />
              <AppSkeleton height={28} variant="circular" width={28} />
            </>
          )}
        </div>
      </div>
    </FrostedCard>
  );
};

export default ComposeStackCard;
