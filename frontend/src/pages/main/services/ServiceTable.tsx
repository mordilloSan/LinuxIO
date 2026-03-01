import GridViewIcon from "@mui/icons-material/GridView";
import MiscellaneousServicesIcon from "@mui/icons-material/MiscellaneousServices";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import RestartAltIcon from "@mui/icons-material/RestartAlt";
import StopCircleIcon from "@mui/icons-material/StopCircle";
import TableRowsIcon from "@mui/icons-material/TableRows";
import TerminalIcon from "@mui/icons-material/Terminal";
import {
  Box,
  Grid,
  TableCell,
  IconButton,
  TextField,
  Tooltip,
  Typography,
  useTheme,
} from "@mui/material";
import { alpha } from "@mui/material/styles";
import { AnimatePresence, motion } from "framer-motion";
import React, { useState, useEffect } from "react";

import ServiceDetailPanel from "./ServiceDetailPanel";

import FrostedCard from "@/components/cards/RootCard";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import { getServiceStatusColor } from "@/constants/statusColors";
import { useViewMode } from "@/hooks/useViewMode";

export interface Service {
  name: string;
  description?: string;
  load_state: string;
  active_state: string;
  sub_state: string;
  [key: string]: unknown;
}

interface ServiceTableProps {
  serviceList: Service[];
  onRestart: (service: Service) => void;
  onStop: (service: Service) => void;
  onStart: (service: Service) => void;
  onViewLogs: (service: Service) => void;
  isLoading?: boolean;
}

const labelSx = {
  textTransform: "uppercase" as const,
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "text.secondary",
  flexShrink: 0,
  width: 44,
};

const ServiceTable: React.FC<ServiceTableProps> = ({
  serviceList,
  onRestart,
  onStop,
  onStart,
  onViewLogs,
  isLoading = false,
}) => {
  const theme = useTheme();
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode("services.list", "table");
  const [expanded, setExpanded] = useState<string | null>(null);

  const filtered = serviceList.filter(
    (s) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      (s.description?.toLowerCase().includes(search.toLowerCase()) ?? false),
  );

  // Close on Escape
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setExpanded(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const expandedService = filtered.find((s) => s.name === expanded) ?? null;

  const columns: UnifiedTableColumn[] = [
    {
      field: "status",
      headerName: "Status",
      align: "left",
      width: "120px",
      sx: { paddingLeft: "8px" },
    },
    { field: "name", headerName: "Name", align: "left", width: "200px" },
    {
      field: "load_state",
      headerName: "Load State",
      align: "left",
      width: "120px",
    },
    {
      field: "sub_state",
      headerName: "Sub State",
      align: "left",
      width: "120px",
    },
    { field: "description", headerName: "Description", align: "left" },
    { field: "actions", headerName: "Actions", align: "right", width: "180px" },
  ];

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
        <Tooltip
          title={
            viewMode === "table"
              ? "Switch to card view"
              : "Switch to table view"
          }
        >
          <IconButton
            size="small"
            onClick={() => {
              setViewMode(viewMode === "table" ? "card" : "table");
              setExpanded(null);
            }}
          >
            {viewMode === "table" ? (
              <GridViewIcon fontSize="small" />
            ) : (
              <TableRowsIcon fontSize="small" />
            )}
          </IconButton>
        </Tooltip>
      </Box>

      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <Grid container spacing={3}>
            <AnimatePresence>
              {filtered.map((service) =>
                expanded && expanded !== service.name ? null : (
                  <Grid
                    key={service.name}
                    size={
                      expanded === service.name
                        ? { xs: 12, md: 4, lg: 3 }
                        : { xs: 12, sm: 6, md: 4, lg: 3 }
                    }
                    component={motion.div}
                    layout
                    initial={{ opacity: 0, scale: 0.95 }}
                    animate={{ opacity: 1, scale: 1 }}
                    exit={{ opacity: 0, scale: 0.9 }}
                    transition={{ duration: 0.2 }}
                  >
                    {(() => {
                      const statusColor = getServiceStatusColor(
                        service.active_state,
                      );
                      const subStateColor =
                        service.sub_state === "running"
                          ? getServiceStatusColor("active")
                          : theme.palette.text.secondary;
                      const isSelected = expanded === service.name;

                      return (
                        <FrostedCard
                          onClick={() =>
                            setExpanded(isSelected ? null : service.name)
                          }
                          sx={{
                            p: 2,
                            display: "flex",
                            flexDirection: "column",
                            height: "100%",
                            cursor: "pointer",
                            transition:
                              "transform 0.2s, box-shadow 0.2s, border 0.3s ease-in-out, margin 0.3s ease-in-out",
                            borderBottomWidth: "2px",
                            borderBottomStyle: "solid",
                            borderBottomColor: isSelected
                              ? statusColor
                              : `color-mix(in srgb, ${statusColor}, transparent 70%)`,
                            ...(!isSelected && {
                              "&:hover": {
                                transform: "translateY(-4px)",
                                boxShadow: `0 8px 24px ${alpha(theme.palette.common.black, 0.35)}`,
                              },
                            }),
                          }}
                        >
                          {/* Header */}
                          <Box
                            sx={{
                              display: "flex",
                              justifyContent: "space-between",
                              alignItems: "center",
                              mb: 1.5,
                              gap: 1,
                            }}
                          >
                            <Box
                              sx={{
                                display: "flex",
                                alignItems: "center",
                                gap: 1,
                                minWidth: 0,
                              }}
                            >
                              <Box
                                component="span"
                                sx={{
                                  display: "inline-block",
                                  width: 8,
                                  height: 8,
                                  borderRadius: "50%",
                                  bgcolor: statusColor,
                                  flexShrink: 0,
                                }}
                              />
                              <Typography
                                variant="body2"
                                fontWeight="bold"
                                noWrap
                                sx={{ minWidth: 0 }}
                              >
                                {service.name}
                              </Typography>
                            </Box>
                            <MiscellaneousServicesIcon
                              sx={{
                                fontSize: 20,
                                color: isSelected
                                  ? statusColor
                                  : "text.disabled",
                                flexShrink: 0,
                                transition: "color 0.2s",
                              }}
                            />
                          </Box>

                          {/* Stat rows */}
                          <Box sx={{ flex: 1 }}>
                            {[
                              {
                                label: "Active",
                                value: service.active_state,
                                color: statusColor,
                              },
                              {
                                label: "Load",
                                value: service.load_state,
                                color:
                                  service.load_state === "loaded"
                                    ? theme.palette.text.primary
                                    : theme.palette.text.secondary,
                              },
                              {
                                label: "Sub",
                                value: service.sub_state,
                                color: subStateColor,
                              },
                            ].map(({ label, value, color }) => (
                              <Box
                                key={label}
                                sx={{
                                  display: "flex",
                                  alignItems: "baseline",
                                  gap: 1,
                                  py: 0.3,
                                  borderBottom: "1px solid",
                                  borderColor: "divider",
                                  "&:last-child": { borderBottom: "none" },
                                }}
                              >
                                <Typography variant="caption" sx={labelSx}>
                                  {label}
                                </Typography>
                                <Typography
                                  variant="body2"
                                  fontWeight={500}
                                  noWrap
                                  sx={{ color }}
                                >
                                  {value}
                                </Typography>
                              </Box>
                            ))}
                          </Box>

                          {/* Description */}
                          {service.description && (
                            <Typography
                              variant="caption"
                              color="text.secondary"
                              noWrap
                              title={service.description}
                              sx={{ display: "block", mt: 1 }}
                            >
                              {service.description}
                            </Typography>
                          )}

                          {/* Actions */}
                          <Box sx={{ display: "flex", gap: 0.5, mt: 1.5 }}>
                            <Tooltip title="View logs">
                              <IconButton
                                size="small"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  onViewLogs(service);
                                }}
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
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      onRestart(service);
                                    }}
                                    disabled={isLoading}
                                  >
                                    <RestartAltIcon fontSize="small" />
                                  </IconButton>
                                </Tooltip>
                                <Tooltip title="Stop">
                                  <IconButton
                                    size="small"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      onStop(service);
                                    }}
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
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    onStart(service);
                                  }}
                                  disabled={isLoading}
                                >
                                  <PlayArrowIcon fontSize="small" />
                                </IconButton>
                              </Tooltip>
                            )}
                          </Box>
                        </FrostedCard>
                      );
                    })()}
                  </Grid>
                ),
              )}

              {/* Detail panel */}
              {expandedService && (
                <Grid
                  key="detail-panel"
                  size={{ xs: 12, md: 8, lg: 9 }}
                  component={motion.div}
                  initial={{ opacity: 0, x: 40 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: 40 }}
                  transition={{ duration: 0.25, delay: 0.05 }}
                >
                  <ServiceDetailPanel
                    service={expandedService}
                    onClose={() => setExpanded(null)}
                    onRestart={onRestart}
                    onStop={onStop}
                    onStart={onStart}
                    isLoading={isLoading}
                  />
                </Grid>
              )}
            </AnimatePresence>
          </Grid>
        ) : (
          <Box textAlign="center" py={4}>
            <Typography variant="body2" color="text.secondary">
              No services found.
            </Typography>
          </Box>
        )
      ) : (
        <UnifiedCollapsibleTable
          data={filtered}
          columns={columns}
          getRowKey={(service) => service.name}
          renderMainRow={(service) => (
            <>
              <TableCell sx={{ paddingLeft: "8px" }}>
                <Box
                  component="span"
                  sx={{
                    display: "inline-block",
                    width: 12,
                    height: 12,
                    borderRadius: "50%",
                    bgcolor: getServiceStatusColor(service.active_state),
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
            </>
          )}
          renderExpandedContent={(service) => (
            <>
              <b>Name:</b> {service.name}
              <br />
              <b>Description:</b> {service.description || "-"}
              <br />
              <b>Load State:</b> {service.load_state}
              <br />
              <b>Active State:</b> {service.active_state}
              <br />
              <b>Sub State:</b> {service.sub_state}
            </>
          )}
          emptyMessage="No services found."
        />
      )}
    </Box>
  );
};

export default ServiceTable;
