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
import React, { useCallback, useState } from "react";

import UnifiedCollapsibleTable from "@/components/tables/UnifiedCollapsibleTable";
import type { UnifiedTableColumn } from "@/components/tables/UnifiedCollapsibleTable";

interface ComposeService {
  name: string;
  image: string;
  status: string;
  state: string;
  container_count: number;
  container_ids: string[];
  ports: string[];
}

export interface ComposeProject {
  name: string;
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
  onDown: (projectName: string) => void;
  onEdit?: (projectName: string, configPath: string) => void;
  isLoading?: boolean;
}

const ComposeList: React.FC<ComposeListProps> = ({
  projects,
  onStart,
  onStop,
  onRestart,
  onDown,
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
      headerName: "Status",
      width: "120px",
    },
    { field: "name", headerName: "Stack Name" },
    { field: "containers", headerName: "Containers", width: "120px" },
    {
      field: "config",
      headerName: "Config Files",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
    { field: "actions", headerName: "Actions", align: "right", width: "180px" },
  ];

  // Render main row content
  const renderMainRow = useCallback(
    (project: ComposeProject) => {
      return (
        <>
          <TableCell>
            <Box sx={{ display: "flex", alignItems: "center" }}>
              <Box
                component="span"
                sx={{
                  display: "inline-block",
                  width: 12,
                  height: 12,
                  borderRadius: "50%",
                  bgcolor: getStatusColor(project.status),
                  mr: { xs: 0, sm: 1 },
                }}
              />
              <Chip
                label={project.status}
                size="small"
                sx={{
                  textTransform: "capitalize",
                  fontSize: "0.75rem",
                  display: { xs: "none", sm: "inline-flex" },
                }}
              />
            </Box>
          </TableCell>
          <TableCell>
            <Typography variant="body2" fontWeight="bold">
              {project.name}
            </Typography>
          </TableCell>
          <TableCell>{getTotalContainers(project)}</TableCell>
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
          <TableCell align="right">
            {onEdit && project.config_files.length > 0 && (
              <Tooltip title="Edit">
                <IconButton
                  size="small"
                  onClick={() => onEdit(project.name, project.config_files[0])}
                  disabled={isLoading}
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
                <Tooltip title="Down (stop & remove)">
                  <IconButton
                    size="small"
                    onClick={() => onDown(project.name)}
                    disabled={isLoading}
                  >
                    <DeleteIcon fontSize="small" />
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
          </TableCell>
        </>
      );
    },
    [onEdit, isLoading, onRestart, onStop, onDown, onStart],
  );

  // Render expanded content
  const renderExpandedContent = useCallback((project: ComposeProject) => {
    return (
      <>
        <Typography variant="subtitle2" gutterBottom>
          <b>Services:</b>
        </Typography>
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
                <TableCell>{service.name}</TableCell>
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
          <Typography variant="body2" color="text.secondary">
            <b>Working Directory:</b> {project.working_dir || "-"}
          </Typography>
          <Typography variant="body2" color="text.secondary">
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
