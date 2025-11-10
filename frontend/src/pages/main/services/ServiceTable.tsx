import RestartAltIcon from "@mui/icons-material/RestartAlt";
import StopCircleIcon from "@mui/icons-material/StopCircle";
import TerminalIcon from "@mui/icons-material/Terminal";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
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
} from "@mui/material";
import { motion } from "framer-motion";
import React, { useState } from "react";

export interface Service {
  name: string;
  description?: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  [key: string]: any;
}

interface ServiceTableProps {
  serviceList: Service[];
  onRestart: (service: Service) => void;
  onStop: (service: Service) => void;
  onStart: (service: Service) => void;
  onViewLogs: (service: Service) => void;
  isLoading?: boolean;
}

const ServiceTable: React.FC<ServiceTableProps> = ({
  serviceList,
  onRestart,
  onStop,
  onStart,
  onViewLogs,
  isLoading = false,
}) => {
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

  const filtered = serviceList.filter(
    (s) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      (s.description?.toLowerCase().includes(search.toLowerCase()) ?? false),
  );

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2}>
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search services…"
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
              <TableCell>Name</TableCell>
              <TableCell>Load State</TableCell>
              <TableCell>Sub State</TableCell>
              <TableCell>Description</TableCell>
              <TableCell align="right">Actions</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {filtered.map((service, index) => (
              <React.Fragment key={service.name}>
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
                    <Box
                      component="span"
                      sx={{
                        display: "inline-block",
                        width: 12,
                        height: 12,
                        borderRadius: "50%",
                        bgcolor:
                          service.active_state === "active"
                            ? "#00e676"
                            : service.active_state === "failed"
                              ? "#ff5252"
                              : "#bdbdbd",
                        mr: 1,
                      }}
                    />
                    {service.active_state}
                  </TableCell>
                  <TableCell>{service.name}</TableCell>
                  <TableCell>{service.load_state}</TableCell>
                  <TableCell>{service.sub_state}</TableCell>
                  <TableCell>{service.description || "-"}</TableCell>
                  <TableCell align="right">
                    <Tooltip title="View logs">
                      <IconButton
                        size="small"
                        onClick={() => onViewLogs(service)}
                        disabled={isLoading}
                      >
                        <TerminalIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                    {service.active_state === "active" ? (
                      <>
                        <Tooltip title="Restart">
                          <IconButton
                            size="small"
                            onClick={() => onRestart(service)}
                            disabled={isLoading}
                          >
                            <RestartAltIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="Stop">
                          <IconButton
                            size="small"
                            onClick={() => onStop(service)}
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
                          onClick={() => onStart(service)}
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
                          expanded === service.name ? null : service.name,
                        )
                      }
                    >
                      <ExpandMoreIcon
                        style={{
                          transform:
                            expanded === service.name
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
                    colSpan={7}
                  >
                    <Collapse
                      in={expanded === service.name}
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
                        }}
                      >
                        <b>Name:</b> {service.name}
                        <br />
                        <b>Description:</b> {service.description || "-"}
                        <br />
                        <b>Load State:</b> {service.load_state}
                        <br />
                        <b>Active State:</b> {service.active_state}
                        <br />
                        <b>Sub State:</b> {service.sub_state}
                        <br />
                        {/* Add whatever extra info you want */}
                      </Box>
                    </Collapse>
                  </TableCell>
                </TableRow>
              </React.Fragment>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  );
};

export default ServiceTable;
