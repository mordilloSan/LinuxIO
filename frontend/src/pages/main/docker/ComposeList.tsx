import DeleteIcon from "@mui/icons-material/Delete";
import EditIcon from "@mui/icons-material/Edit";
import FolderOpenIcon from "@mui/icons-material/FolderOpen";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import RestartAltIcon from "@mui/icons-material/RestartAlt";
import StopCircleIcon from "@mui/icons-material/StopCircle";
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  IconButton,
  TextField,
  Tooltip,
  Chip,
  Typography,
} from "@mui/material";
import { alpha } from "@mui/material/styles";
import React, { useCallback, useState } from "react";

import DockerIcon from "@/components/docker/DockerIcon";
import UnifiedCollapsibleTable from "@/components/tables/UnifiedCollapsibleTable";
import type { UnifiedTableColumn } from "@/components/tables/UnifiedCollapsibleTable";

interface ComposeService {
  name: string;
  image: string;
  icon?: string;
  url?: string;
  status: string;
  state: string;
  container_count: number;
  container_ids: string[];
  ports: string[];
}

export interface ComposeProject {
  name: string;
  icon?: string;
  status: string; // "running", "partial", "stopped"
  services: Record<string, ComposeService>;
  config_files: string[];
  working_dir: string;
}

interface ComposeListProps {
  projects: ComposeProject[];
  onStart: (projectName: string) => void;
  onStop: (projectName: string) => void;
  onRestart: (projectName: string) => void;
  onDelete: (project: ComposeProject) => void;
  onEdit?: (projectName: string, configPath: string) => void;
  isLoading?: boolean;
}

const ComposeList: React.FC<ComposeListProps> = ({
  projects,
  onStart,
  onStop,
  onRestart,
  onDelete,
  onEdit,
  isLoading = false,
}) => {
  const [search, setSearch] = useState("");

  const filtered = projects.filter((p) =>
    p.name.toLowerCase().includes(search.toLowerCase()),
  );

  const getStatusColor = (status: string) => {
    switch (status) {
      case "running":
        return "#00e676";
      case "partial":
        return "#ffc107";
      case "stopped":
        return "#bdbdbd";
      default:
        return "#bdbdbd";
    }
  };

  const getTotalContainers = (project: ComposeProject) => {
    return Object.values(project.services).reduce(
      (acc, service) => acc + service.container_count,
      0,
    );
  };

  // Table columns configuration
  const columns: UnifiedTableColumn[] = [
    {
      field: "status",
      headerName: "",
      width: "40px",
    },
    { field: "name", headerName: "Stack" },
    {
      field: "containers",
      headerName: "Containers",
      width: "100px",
      align: "center",
    },
    {
      field: "config",
      headerName: "Config Files",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
    {
      field: "location",
      headerName: "Location",
      sx: { display: { xs: "none", lg: "table-cell" } },
    },
    {
      field: "actions",
      headerName: "Actions",
      align: "center",
      width: "200px",
    },
  ];

  // Render main row content
  const renderMainRow = useCallback(
    (project: ComposeProject) => {
      const statusColor = getStatusColor(project.status);

      return (
        <>
          <TableCell sx={{ px: { xs: 1, sm: 2 }, py: { xs: 1.5, sm: 2 } }}>
            <Box sx={{ display: "flex", alignItems: "center" }}>
              <Box
                component="span"
                sx={{
                  display: { xs: "inline-block", sm: "none" },
                  width: 10,
                  height: 10,
                  borderRadius: "50%",
                  bgcolor: statusColor,
                }}
              />
              <Chip
                label={project.status}
                size="small"
                sx={{
                  display: { xs: "none", sm: "inline-flex" },
                  textTransform: "capitalize",
                  fontSize: "0.68rem",
                  fontWeight: 500,
                  color: statusColor,
                  bgcolor: alpha(statusColor, 0.14),
                  border: `1px solid ${alpha(statusColor, 0.45)}`,
                  borderRadius: "999px",
                  "& .MuiChip-label": {
                    px: 3,
                  },
                }}
              />
            </Box>
          </TableCell>
          <TableCell>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
              <DockerIcon
                identifier={project.icon}
                size={28}
                alt={project.name}
              />
              <Typography variant="body2" fontWeight="bold">
                {project.name}
              </Typography>
            </Box>
          </TableCell>
          <TableCell align="center">{getTotalContainers(project)}</TableCell>
          <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
            <Tooltip title={project.config_files.join(", ") || "Unknown"}>
              <Box sx={{ display: "flex", alignItems: "center" }}>
                <FolderOpenIcon
                  fontSize="small"
                  sx={{ mr: 0.5, opacity: 0.7 }}
                />
                <Typography variant="body2" noWrap sx={{ maxWidth: 200 }}>
                  {project.config_files[0]?.split("/").pop() ||
                    "docker-compose.yml"}
                </Typography>
              </Box>
            </Tooltip>
          </TableCell>
          <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
            <Tooltip title={project.working_dir || "Unknown"}>
              <Typography
                variant="body2"
                noWrap
                sx={{
                  maxWidth: 600,
                  fontSize: "0.85rem",
                  color: "text.secondary",
                }}
              >
                {project.working_dir || "-"}
              </Typography>
            </Tooltip>
          </TableCell>
          <TableCell align="right">
            <Box
              sx={{
                display: "flex",
                justifyContent: "flex-end",
                gap: { xs: 0, sm: 0.5 },
              }}
            >
              {onEdit && project.config_files.length > 0 && (
                <Tooltip title="Edit">
                  <IconButton
                    size="small"
                    onClick={() =>
                      onEdit(project.name, project.config_files[0])
                    }
                    disabled={isLoading}
                    sx={{ p: { xs: 0.5, sm: 1 } }}
                  >
                    <EditIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              )}
              {project.status === "running" || project.status === "partial" ? (
                <>
                  <Tooltip title="Restart">
                    <IconButton
                      size="small"
                      onClick={() => onRestart(project.name)}
                      disabled={isLoading}
                      sx={{ p: { xs: 0.5, sm: 1 } }}
                    >
                      <RestartAltIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title="Stop">
                    <IconButton
                      size="small"
                      onClick={() => onStop(project.name)}
                      disabled={isLoading}
                      sx={{ p: { xs: 0.5, sm: 1 } }}
                    >
                      <StopCircleIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title="Delete">
                    <IconButton
                      size="small"
                      onClick={() => onDelete(project)}
                      disabled={isLoading}
                      sx={{ p: { xs: 0.5, sm: 1 } }}
                    >
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </>
              ) : (
                <>
                  <Tooltip title="Start">
                    <IconButton
                      size="small"
                      onClick={() => onStart(project.name)}
                      disabled={isLoading}
                      sx={{ p: { xs: 0.5, sm: 1 } }}
                    >
                      <PlayArrowIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title="Delete">
                    <IconButton
                      size="small"
                      onClick={() => onDelete(project)}
                      disabled={isLoading}
                      sx={{ p: { xs: 0.5, sm: 1 } }}
                    >
                      <DeleteIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </>
              )}
            </Box>
          </TableCell>
        </>
      );
    },
    [onEdit, isLoading, onRestart, onStop, onDelete, onStart],
  );

  // Render expanded content
  const renderExpandedContent = useCallback((project: ComposeProject) => {
    return (
      <>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Service Name</TableCell>
              <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
                Image
              </TableCell>
              <TableCell>State</TableCell>
              <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                Containers
              </TableCell>
              <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                Ports
              </TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {Object.values(project.services).map((service) => (
              <TableRow key={service.name}>
                <TableCell>
                  <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                    <DockerIcon
                      identifier={service.icon}
                      size={20}
                      alt={service.name}
                    />
                    {service.name}
                  </Box>
                </TableCell>
                <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
                  <Typography variant="body2" noWrap sx={{ maxWidth: 200 }}>
                    {service.image}
                  </Typography>
                </TableCell>
                <TableCell>
                  <Chip
                    label={service.state}
                    size="small"
                    color={service.state === "running" ? "success" : "default"}
                    sx={{ textTransform: "capitalize" }}
                  />
                </TableCell>
                <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                  {service.container_count}
                </TableCell>
                <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                  {service.ports.length > 0 ? service.ports.join(", ") : "-"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <Box mt={2}>
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ wordBreak: "break-word", overflowWrap: "break-word" }}
          >
            <b>Working Directory:</b> {project.working_dir || "-"}
          </Typography>
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ wordBreak: "break-word", overflowWrap: "break-word" }}
          >
            <b>Config Files:</b> {project.config_files.join(", ") || "-"}
          </Typography>
        </Box>
      </>
    );
  }, []);

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2}>
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search stacks…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          sx={{ width: 320 }}
        />
        <Box fontWeight="bold">{filtered.length} shown</Box>
      </Box>
      <UnifiedCollapsibleTable
        data={filtered}
        columns={columns}
        getRowKey={(project) => project.name}
        renderMainRow={renderMainRow}
        renderExpandedContent={renderExpandedContent}
        emptyMessage="No compose stacks found. Start containers with docker compose to see them here."
      />
    </Box>
  );
};

export default ComposeList;
