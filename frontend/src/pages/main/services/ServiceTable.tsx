import GridViewIcon from "@mui/icons-material/GridView";
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
} from "@mui/material";
import React, { useState } from "react";

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
  const [viewMode, setViewMode] = useViewMode("services.list", "table");

  const filtered = serviceList.filter(
    (s) =>
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      (s.description?.toLowerCase().includes(search.toLowerCase()) ?? false),
  );

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
            onClick={() => setViewMode(viewMode === "table" ? "card" : "table")}
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
          <Grid container spacing={2}>
            {filtered.map((service) => (
              <Grid key={service.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <FrostedCard sx={{ p: 2 }}>
                  <Box
                    sx={{
                      display: "flex",
                      justifyContent: "space-between",
                      alignItems: "center",
                      gap: 1,
                      mb: 1,
                    }}
                  >
                    <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                      <Box
                        component="span"
                        sx={{
                          display: "inline-block",
                          width: 10,
                          height: 10,
                          borderRadius: "50%",
                          bgcolor: getServiceStatusColor(service.active_state),
                        }}
                      />
                      <Typography variant="body2" fontWeight="bold" noWrap>
                        {service.name}
                      </Typography>
                    </Box>
                    <Typography variant="caption" color="text.secondary">
                      {service.active_state}
                    </Typography>
                  </Box>

                  <Typography variant="body2">
                    Load: {service.load_state}
                  </Typography>
                  <Typography variant="body2">
                    Sub-state: {service.sub_state}
                  </Typography>
                  <Typography variant="body2" sx={{ mb: 1 }}>
                    {service.description || "-"}
                  </Typography>

                  <Box sx={{ display: "flex", gap: 0.5 }}>
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
                  </Box>
                </FrostedCard>
              </Grid>
            ))}
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
