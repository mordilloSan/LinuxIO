import { memo } from "react";

import type { ComposeProject } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import DockerIcon from "@/components/docker/DockerIcon";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";
import { getComposeStatusColor } from "@/constants/statusColors";

const getStatusColor = (status: string) => {
  return getComposeStatusColor(status);
};

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

interface ComposeStackCardProps {
  project: ComposeProject;
  onStart: (projectName: string) => void;
  onStop: (projectName: string) => void;
  onRestart: (projectName: string) => void;
  onDelete: (project: ComposeProject) => void;
  onEdit?: (projectName: string, configPath: string) => void;
  isLoading?: boolean;
}

function areStringArraysEqual(previous: string[], next: string[]) {
  return (
    previous.length === next.length &&
    previous.every((value, index) => value === next[index])
  );
}

function areDisplayedServicesEqual(
  previous: ComposeProject["services"],
  next: ComposeProject["services"],
) {
  const previousEntries = Object.entries(previous);
  if (previousEntries.length !== Object.keys(next).length) return false;

  return previousEntries.every(([name, service]) => {
    const nextService = next[name];
    return (
      nextService !== undefined &&
      service.container_count === nextService.container_count &&
      service.state === nextService.state
    );
  });
}

function areComposeStackCardPropsEqual(
  previous: ComposeStackCardProps,
  next: ComposeStackCardProps,
) {
  if (
    previous.isLoading !== next.isLoading ||
    previous.onDelete !== next.onDelete ||
    previous.onEdit !== next.onEdit ||
    previous.onRestart !== next.onRestart ||
    previous.onStart !== next.onStart ||
    previous.onStop !== next.onStop
  ) {
    return false;
  }

  const previousProject = previous.project;
  const nextProject = next.project;
  if (previousProject === nextProject) return true;

  // Containers are intentionally excluded. Their status details feed the
  // expanded table, while this card only renders the project summary below.
  // The delete flow reads the compared name/config/location fields.
  return (
    previousProject.icon === nextProject.icon &&
    previousProject.name === nextProject.name &&
    previousProject.status === nextProject.status &&
    previousProject.update_available === nextProject.update_available &&
    previousProject.working_dir === nextProject.working_dir &&
    areStringArraysEqual(
      previousProject.config_files,
      nextProject.config_files,
    ) &&
    areDisplayedServicesEqual(previousProject.services, nextProject.services)
  );
}

const ComposeStackCard = ({
  project,
  onStart,
  onStop,
  onRestart,
  onDelete,
  onEdit,
  isLoading = false,
}: ComposeStackCardProps) => {
  const totalContainers = Object.values(project.services).reduce(
    (acc, service) => acc + service.container_count,
    0,
  );
  const runningServices = Object.values(project.services).filter(
    (service) => service.state === "running",
  ).length;
  const totalServices = Object.keys(project.services).length;
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
        <AppTypography
          fontWeight={600}
          noWrap
          title={project.name}
          toastMeta={DOCKER_TOAST_META}
          variant="subtitle1"
        >
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
        {project.update_available && (
          <Chip
            color="warning"
            label="Update available"
            size="small"
            style={{ fontSize: "0.68rem" }}
            variant="soft"
          />
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
                  onClick={() => onRestart(project.name)}
                />
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:stop-circle"
                  iconSize={20}
                  label="Stop"
                  onClick={() => onStop(project.name)}
                />
              </>
            ) : (
              <AppActionIconButton
                disabled={isLoading}
                icon="mdi:play"
                iconSize={20}
                label="Start"
                onClick={() => onStart(project.name)}
              />
            )}
            <AppActionIconButton
              disabled={isLoading}
              icon="mdi:delete"
              iconSize={20}
              label="Delete"
              onClick={() => onDelete(project)}
            />
          </>
        </div>
      </div>
    </FrostedCard>
  );
};

export default memo(ComposeStackCard, areComposeStackCardPropsEqual);
