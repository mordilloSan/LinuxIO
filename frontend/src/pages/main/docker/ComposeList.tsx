import DeleteIcon from "@mui/icons-material/Delete";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import FolderOpenIcon from "@mui/icons-material/FolderOpen";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import RestartAltIcon from "@mui/icons-material/RestartAlt";
import StopCircleIcon from "@mui/icons-material/StopCircle";
import {
  Box,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  TextField,
  Tooltip,
  Collapse,
  Chip,
  Typography,
} from "@mui/material";
import { motion } from "framer-motion";
import React, { useState } from "react";

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
  isLoading?: boolean;
}

const ComposeList: React.FC<ComposeListProps> = ({
  projects,
  onStart,
  onStop,
  onRestart,
  onDown,
  isLoading = false,
}) => {
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

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
      <TableContainer>
        <Table size="small" sx={{ borderRadius: 3, boxShadow: 2 }}>
          <TableHead>
            <TableRow
              sx={(theme) => ({
                "& .MuiTableCell-root": { borderBottom: "none" },
                backgroundColor:
                  theme.palette.mode === "dark"
                    ? "rgba(255,255,255,0.08)"
                    : "rgba(0,0,0,0.08)",
                borderRadius: "6px",
                boxShadow: "none",
              })}
            >
              <TableCell>Status</TableCell>
              <TableCell>Stack Name</TableCell>
              <TableCell>Containers</TableCell>
              <TableCell>Config Files</TableCell>
              <TableCell align="right">Actions</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {filtered.map((project, index) => (
              <React.Fragment key={project.name}>
                <TableRow
                  sx={(theme) => ({
                    "& .MuiTableCell-root": { borderBottom: "none" },
                    backgroundColor:
                      index % 2 === 0
                        ? "transparent"
                        : theme.palette.mode === "dark"
                          ? "rgba(255,255,255,0.04)"
                          : "rgba(0,0,0,0.05)",
                  })}
                >
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
                          mr: 1,
                        }}
                      />
                      <Chip
                        label={project.status}
                        size="small"
                        sx={{
                          textTransform: "capitalize",
                          fontSize: "0.75rem",
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
                  <TableCell>
                    <Tooltip
                      title={project.config_files.join(", ") || "Unknown"}
                    >
                      <Box sx={{ display: "flex", alignItems: "center" }}>
                        <FolderOpenIcon
                          fontSize="small"
                          sx={{ mr: 0.5, opacity: 0.7 }}
                        />
                        <Typography
                          variant="body2"
                          noWrap
                          sx={{ maxWidth: 200 }}
                        >
                          {project.config_files[0]?.split("/").pop() ||
                            "docker-compose.yml"}
                        </Typography>
                      </Box>
                    </Tooltip>
                  </TableCell>
                  <TableCell align="right">
                    {project.status === "running" ||
                    project.status === "partial" ? (
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
                  <TableCell>
                    <IconButton
                      size="small"
                      onClick={() =>
                        setExpanded(
                          expanded === project.name ? null : project.name,
                        )
                      }
                    >
                      <ExpandMoreIcon
                        style={{
                          transform:
                            expanded === project.name
                              ? "rotate(180deg)"
                              : "rotate(0deg)",
                          transition: "0.2s",
                        }}
                      />
                    </IconButton>
                  </TableCell>
                </TableRow>
                <TableRow
                  sx={(theme) => ({
                    "& .MuiTableCell-root": { borderBottom: "none" },
                    backgroundColor:
                      index % 2 === 0
                        ? "transparent"
                        : theme.palette.mode === "dark"
                          ? "rgba(255,255,255,0.08)"
                          : "rgba(0,0,0,0.05)",
                  })}
                >
                  <TableCell
                    style={{ paddingBottom: 0, paddingTop: 0 }}
                    colSpan={6}
                  >
                    <Collapse
                      in={expanded === project.name}
                      timeout="auto"
                      unmountOnExit
                    >
                      <Box
                        component={motion.div}
                        initial={{ opacity: 0, y: -10 }}
                        animate={{ opacity: 1, y: 0 }}
                        sx={{
                          margin: 2,
                          borderRadius: 2,
                          p: 2,
                          bgcolor: (theme) =>
                            theme.palette.mode === "dark"
                              ? "rgba(255,255,255,0.05)"
                              : "rgba(0,0,0,0.03)",
                        }}
                      >
                        <Typography variant="subtitle2" gutterBottom>
                          <b>Services:</b>
                        </Typography>
                        <Table size="small">
                          <TableHead>
                            <TableRow>
                              <TableCell>Service Name</TableCell>
                              <TableCell>Image</TableCell>
                              <TableCell>State</TableCell>
                              <TableCell>Containers</TableCell>
                              <TableCell>Ports</TableCell>
                            </TableRow>
                          </TableHead>
                          <TableBody>
                            {Object.values(project.services).map((service) => (
                              <TableRow key={service.name}>
                                <TableCell>{service.name}</TableCell>
                                <TableCell>
                                  <Typography
                                    variant="body2"
                                    noWrap
                                    sx={{ maxWidth: 200 }}
                                  >
                                    {service.image}
                                  </Typography>
                                </TableCell>
                                <TableCell>
                                  <Chip
                                    label={service.state}
                                    size="small"
                                    color={
                                      service.state === "running"
                                        ? "success"
                                        : "default"
                                    }
                                    sx={{ textTransform: "capitalize" }}
                                  />
                                </TableCell>
                                <TableCell>{service.container_count}</TableCell>
                                <TableCell>
                                  {service.ports.length > 0
                                    ? service.ports.join(", ")
                                    : "-"}
                                </TableCell>
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                        <Box mt={2}>
                          <Typography variant="body2" color="text.secondary">
                            <b>Working Directory:</b>{" "}
                            {project.working_dir || "-"}
                          </Typography>
                          <Typography variant="body2" color="text.secondary">
                            <b>Config Files:</b>{" "}
                            {project.config_files.join(", ") || "-"}
                          </Typography>
                        </Box>
                      </Box>
                    </Collapse>
                  </TableCell>
                </TableRow>
              </React.Fragment>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {filtered.length === 0 && (
        <Box textAlign="center" py={4}>
          <Typography variant="body2" color="text.secondary">
            No compose stacks found. Start containers with docker compose to see
            them here.
          </Typography>
        </Box>
      )}
    </Box>
  );
};

export default ComposeList;
