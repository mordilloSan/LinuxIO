import DeleteIcon from "@mui/icons-material/Delete";
import EditIcon from "@mui/icons-material/Edit";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import RestartAltIcon from "@mui/icons-material/RestartAlt";
import StopCircleIcon from "@mui/icons-material/StopCircle";
import {
  Box,
  Chip,
  Divider,
  IconButton,
  Tooltip,
  Typography,
} from "@mui/material";
import { alpha } from "@mui/material/styles";
import React from "react";

import type { ComposeProject } from "./ComposeList";

import FrostedCard from "@/components/cards/RootCard";
import DockerIcon from "@/components/docker/DockerIcon";
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
      sx={{
        p: 2,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        position: "relative",
        transition: "transform 0.2s, box-shadow 0.2s",
        "&:hover": {
          transform: "translateY(-4px)",
          boxShadow: "0 8px 24px rgba(var(--mui-palette-common-blackChannel) / 0.35)",
        },
      }}
    >
      {/* Status chip top-right */}
      <Box sx={{ position: "absolute", top: 12, right: 12 }}>
        <Chip
          label={project.status}
          size="small"
          sx={{
            textTransform: "capitalize",
            fontSize: "0.65rem",
            fontWeight: 500,
            color: statusColor,
            bgcolor: alpha(statusColor, 0.14),
            border: `1px solid ${alpha(statusColor, 0.45)}`,
            borderRadius: "999px",
            "& .MuiChip-label": { px: 1.5 },
          }}
        />
      </Box>

      {/* Icon + Name */}
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, pr: 8 }}>
        <DockerIcon identifier={project.icon} size={36} alt={project.name} />
        <Typography variant="subtitle1" fontWeight={600} noWrap>
          {project.name}
        </Typography>
      </Box>

      {/* Stats */}
      <Box sx={{ mt: 1.5, display: "flex", gap: 2 }}>
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
      </Box>

      <Divider sx={{ my: 1.5 }} />

      {/* Actions */}
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          mt: "auto",
        }}
      >
        {isWatchtower ? (
          <Tooltip title="View compose file">
            <Chip
              label="Managed by LinuxIO"
              size="small"
              variant="outlined"
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
            <Box sx={{ display: "flex", gap: 0.5 }}>
              {onEdit && project.config_files.length > 0 && (
                <Tooltip title="Edit">
                  <IconButton
                    size="small"
                    onClick={() =>
                      onEdit(project.name, project.config_files[0])
                    }
                    disabled={isLoading}
                  >
                    <EditIcon fontSize="small" />
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
                      <RestartAltIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title="Stop">
                    <IconButton
                      size="small"
                      onClick={() => onStop(project.name)}
                      disabled={isLoading}
                    >
                      <StopCircleIcon fontSize="small" />
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
                    <PlayArrowIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              )}
              <Tooltip title="Delete">
                <IconButton
                  size="small"
                  onClick={() => onDelete(project)}
                  disabled={isLoading}
                >
                  <DeleteIcon fontSize="small" />
                </IconButton>
              </Tooltip>
            </Box>
          </>
        )}
      </Box>
    </FrostedCard>
  );
};

export default ComposeStackCard;
