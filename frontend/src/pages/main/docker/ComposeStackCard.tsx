import { Icon } from "@iconify/react";
import { Divider, IconButton, Tooltip, Typography } from "@mui/material";
import React from "react";

import type { ComposeProject } from "./ComposeList";

import FrostedCard from "@/components/cards/RootCard";
import DockerIcon from "@/components/docker/DockerIcon";
import Chip from "@/components/ui/AppChip";
import { getComposeStatusColor } from "@/constants/statusColors";

const getStatusColor = (status: string) => {
  return getComposeStatusColor(status);
};

interface ComposeStackCardProps {
  project: ComposeProject;
  onStart: (projectName: string) => void;
  onStop: (projectName: string) => void;
  onRestart: (projectName: string) => void;
  onDelete: (project: ComposeProject) => void;
  onEdit?: (projectName: string, configPath: string) => void;
  onPreview?: (projectName: string, configPath: string) => void;
  isLoading?: boolean;
}

const ComposeStackCard: React.FC<ComposeStackCardProps> = ({
  project,
  onStart,
  onStop,
  onRestart,
  onDelete,
  onEdit,
  onPreview,
  isLoading = false,
}) => {
  const statusColor = getStatusColor(project.status);

  const totalContainers = Object.values(project.services).reduce(
    (acc, s) => acc + s.container_count,
    0,
  );
  const runningServices = Object.values(project.services).filter(
    (s) => s.state === "running",
  ).length;
  const totalServices = Object.keys(project.services).length;

  const isWatchtower = project.name === "linuxio-watchtower";
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
        <Typography variant="subtitle1" fontWeight={600} noWrap>
          {project.name}
        </Typography>
      </div>

      {/* Stats */}
      <div style={{ marginTop: 6, display: "flex", gap: 8 }}>
        <Typography variant="body2" color="text.secondary">
          {totalServices > 0
            ? `${runningServices}/${totalServices} services`
            : "No services"}
        </Typography>
        {totalContainers > 0 && (
          <Typography variant="body2" color="text.secondary">
            {totalContainers} container{totalContainers !== 1 ? "s" : ""}
          </Typography>
        )}
      </div>

      <Divider sx={{ my: 1.5 }} />

      {/* Actions */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginTop: "auto",
        }}
      >
        {isWatchtower ? (
          <Tooltip title="View compose file">
            <Chip
              label="Managed by LinuxIO"
              size="small"
              variant="soft"
              onClick={
                onPreview && project.config_files.length > 0
                  ? () => onPreview(project.name, project.config_files[0])
                  : undefined
              }
              sx={{
                fontSize: "0.68rem",
                opacity: 0.7,
                cursor:
                  onPreview && project.config_files.length > 0
                    ? "pointer"
                    : "default",
                "&:hover": { opacity: 1 },
              }}
            />
          </Tooltip>
        ) : (
          <>
            <div style={{ display: "flex", gap: 2 }}>
              {onEdit && project.config_files.length > 0 && (
                <Tooltip title="Edit">
                  <IconButton
                    size="small"
                    onClick={() =>
                      onEdit(project.name, project.config_files[0])
                    }
                    disabled={isLoading}
                  >
                    <Icon icon="mdi:pencil" width={20} height={20} />
                  </IconButton>
                </Tooltip>
              )}
              {isRunning ? (
                <>
                  <Tooltip title="Restart">
                    <IconButton
                      size="small"
                      onClick={() => onRestart(project.name)}
                      disabled={isLoading}
                    >
                      <Icon icon="mdi:restart" width={20} height={20} />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title="Stop">
                    <IconButton
                      size="small"
                      onClick={() => onStop(project.name)}
                      disabled={isLoading}
                    >
                      <Icon icon="mdi:stop-circle" width={20} height={20} />
                    </IconButton>
                  </Tooltip>
                </>
              ) : (
                <Tooltip title="Start">
                  <IconButton
                    size="small"
                    onClick={() => onStart(project.name)}
                    disabled={isLoading}
                  >
                    <Icon icon="mdi:play" width={20} height={20} />
                  </IconButton>
                </Tooltip>
              )}
              <Tooltip title="Delete">
                <IconButton
                  size="small"
                  onClick={() => onDelete(project)}
                  disabled={isLoading}
                >
                  <Icon icon="mdi:delete" width={20} height={20} />
                </IconButton>
              </Tooltip>
            </div>
          </>
        )}
      </div>
    </FrostedCard>
  );
};

export default ComposeStackCard;
