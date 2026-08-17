import { memo } from "react";

import type { ComposeProject } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import DockerIcon from "@/components/docker/DockerIcon";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";
import { getComposeStatusColor } from "@/constants/statusColors";
import { CARD_PADDING_SM } from "@/theme/constants";

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
  onOpen?: () => void;
  selected?: boolean;
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
    previous.onStop !== next.onStop ||
    previous.onOpen !== next.onOpen ||
    previous.selected !== next.selected
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
  onOpen,
  selected = false,
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
  const summary = (
    <>
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
    </>
  );

  return (
    <FrostedCard
      accent
      hoverLift={!selected}
      style={{
        padding: CARD_PADDING_SM,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minWidth: 0,
        position: "relative",
        width: "100%",
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

      {onOpen ? (
        <AppButton
          aria-label={`Open stack ${project.name} details`}
          color="inherit"
          fullWidth
          onClick={onOpen}
          style={{
            alignItems: "stretch",
            color: "inherit",
            flexDirection: "column",
            justifyContent: "flex-start",
            padding: 0,
            textAlign: "left",
          }}
        >
          {summary}
        </AppButton>
      ) : (
        summary
      )}

      <AppDivider style={{ marginBlock: 12 }} />

      {selected && (
        <div style={{ display: "flex", flexDirection: "column" }}>
          <DetailRow label="Compose files" noBorder>
            {project.config_files.length > 0 ? (
              <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                {project.config_files.map((filePath) => (
                  <AppTypography
                    className="expand-panel__mono"
                    component="span"
                    copyText={filePath}
                    fontSize="0.75rem"
                    fontWeight={500}
                    key={filePath}
                    noWrap
                    title={filePath}
                    variant="body2"
                  >
                    {filePath}
                  </AppTypography>
                ))}
              </div>
            ) : (
              <AppTypography
                color="text.secondary"
                component="span"
                fontSize="0.75rem"
                variant="body2"
              >
                No compose files found.
              </AppTypography>
            )}
          </DetailRow>
          <DetailRow label="Location">
            <AppTypography
              className="expand-panel__mono"
              component="span"
              copyText={project.working_dir}
              fontSize="0.75rem"
              fontWeight={500}
              noWrap
              title={project.working_dir}
              variant="body2"
            >
              {project.working_dir || "-"}
            </AppTypography>
          </DetailRow>
        </div>
      )}

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
                  icon="mdi:stop"
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
