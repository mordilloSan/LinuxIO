import { Box, Grid, Tooltip, Typography, Fade } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import React from "react";

import ActionButton from "../../pages/main/docker/ActionButton";
import LogsDialog from "../../pages/main/docker/LogsDialog";
import ComponentLoader from "../loaders/ComponentLoader";

import FrostedCard from "@/components/cards/RootCard";
import MetricBar from "@/components/gauge/MetricBar";
import TerminalDialog from "@/pages/main/docker/TerminalDialog";
import { ContainerInfo } from "@/types/container";
import axios from "@/utils/axios";
import { formatBytes } from "@/utils/formatBytes";

const getContainerIconUrl = (name: string) => {
  const sanitized = name.replace(/[^a-zA-Z0-9-]/g, "").toLowerCase();
  return `https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main/svg/${sanitized}.svg`;
};

const fallbackDockerIcon =
  "https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main/svg/docker.svg";

const getStatusColor = (container: ContainerInfo) => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "warning.main";
  if (status.includes("healthy")) return "success.main";
  if (container.State === "running") return "success.main";
  if (container.State === "exited" || container.State === "dead")
    return "error.main";
  return "warning.main";
};

const getStatusTooltip = (container: ContainerInfo) => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "Unhealthy";
  if (status.includes("healthy")) return "Healthy";
  if (container.State === "running") return "Running";
  if (container.State === "exited") return "Stopped";
  if (container.State === "dead") return "Dead";
  return "Unhealthy / Starting";
};

interface ContainerCardProps {
  container: ContainerInfo;
}

const ContainerCard: React.FC<ContainerCardProps> = ({ container }) => {
  const queryClient = useQueryClient();
  const [loading, setLoading] = React.useState(false);
  const [logDialogOpen, setLogDialogOpen] = React.useState(false);
  const [terminalOpen, setTerminalOpen] = React.useState(false);
  const [logs, setLogs] = React.useState<string | null>(null);
  const [logsLoading, setLogsLoading] = React.useState(false);
  const [logsError, setLogsError] = React.useState<string | null>(null);

  const theme = useTheme();
  const name = container.Names?.[0]?.replace("/", "") || "Unnamed";
  const iconUrl = getContainerIconUrl(name);

  const logsRef = React.useRef<string | null>(logs);
  React.useEffect(() => {
    logsRef.current = logs;
  }, [logs]);
  const fetchLogs = async (id: string) => {
    // Only show spinner if logs are not loaded yet
    if (logs == null) setLogsLoading(true);
    try {
      const res = await axios.get(`/docker/containers/${id}/logs`);
      const output =
        typeof res.data === "string"
          ? res.data
          : (res.data?.output ?? JSON.stringify(res.data, null, 2));
      if (output !== logsRef.current) {
        setLogs(output);
      }
      setLogsError(null);
    } catch (e: any) {
      setLogsError(
        e?.response?.data?.error ||
          e?.message ||
          "Failed to load logs. (Check backend logs for details.)",
      );
    }
    setLogsLoading(false);
  };

  const handleAction = async (
    id: string,
    action: "start" | "stop" | "restart" | "remove" | "exec",
  ) => {
    setLoading(true);
    try {
      await axios.post(`/docker/containers/${id}/${action}`);
      queryClient.invalidateQueries({ queryKey: ["containers"] });
    } finally {
      setLoading(false);
    }
  };

  const handleLogsClick = () => {
    setLogDialogOpen(true);
    fetchLogs(container.Id);
  };

  // Metrics
  const cpuPercent = container.metrics?.cpu_percent ?? 0;
  const memUsage = container.metrics?.mem_usage ?? 0;
  const memLimit = container.metrics?.mem_limit ?? 0;
  const memPercent =
    memLimit > 0 ? Math.min((memUsage / memLimit) * 100, 100) : 0;

  return (
    <Grid size={{ xs: 12, sm: 4, md: 4, lg: 3, xl: 2 }}>
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
            boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
          },
        }}
      >
        {/* Status dot */}
        <Tooltip
          title={getStatusTooltip(container)}
          placement="top"
          arrow
          slots={{ transition: Fade }}
          slotProps={{ transition: { timeout: 300 } }}
        >
          <Box
            sx={{
              position: "absolute",
              top: 18,
              right: 8,
              width: 10,
              height: 10,
              borderRadius: "50%",
              backgroundColor: getStatusColor(container),
              cursor: "default",
            }}
          />
        </Tooltip>

        {/* Top row: Icon + Name + Buttons */}
        <Box
          sx={{
            display: "flex",
            flexDirection: "row",
            alignItems: "center",
            width: "100%",
          }}
        >
          <Box
            component="img"
            src={iconUrl}
            alt={name}
            sx={{
              width: 48,
              height: 48,
              minWidth: 48,
              minHeight: 48,
              objectFit: "contain",
              flexShrink: 0,
              mr: 1.5,
              alignSelf: "flex-start",
            }}
            onError={(e) => {
              (e.currentTarget as HTMLImageElement).src = fallbackDockerIcon;
            }}
          />
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography
              variant="subtitle1"
              fontWeight="600"
              noWrap
              sx={{ ml: 1, mb: 0.5, fontSize: "1.05rem" }}
            >
              {name}
            </Typography>
            <Box sx={{ display: "flex", gap: 0.5 }}>
              {container.State !== "running" && (
                <Tooltip title="Start Container" arrow>
                  <span>
                    <ActionButton
                      icon="mdi:play"
                      onClick={() => handleAction(container.Id, "start")}
                    />
                  </span>
                </Tooltip>
              )}
              {container.State === "running" && (
                <Tooltip title="Stop Container" arrow>
                  <span>
                    <ActionButton
                      icon="mdi:stop"
                      onClick={() => handleAction(container.Id, "stop")}
                    />
                  </span>
                </Tooltip>
              )}
              <Tooltip title="Restart Container" arrow>
                <span>
                  <ActionButton
                    icon="mdi:restart"
                    onClick={() => handleAction(container.Id, "restart")}
                  />
                </span>
              </Tooltip>
              <Tooltip title="Remove Container" arrow>
                <span>
                  <ActionButton
                    icon="mdi:delete"
                    onClick={() => handleAction(container.Id, "remove")}
                  />
                </span>
              </Tooltip>
              <Tooltip title="View Logs" arrow>
                <span>
                  <ActionButton
                    icon="mdi:file-document-outline"
                    onClick={() => handleLogsClick()}
                  />
                </span>
              </Tooltip>
              <Tooltip title="Open Terminal" arrow>
                <span>
                  <ActionButton
                    icon="mdi:console"
                    onClick={() => setTerminalOpen(true)}
                  />
                </span>
              </Tooltip>
            </Box>
          </Box>
        </Box>
        <LogsDialog
          open={logDialogOpen}
          onClose={() => setLogDialogOpen(false)}
          logs={logs}
          loading={logsLoading}
          error={logsError}
          containerName={name}
          onRefresh={() => fetchLogs(container.Id)}
          autoRefreshDefault={true}
        />
        <TerminalDialog
          open={terminalOpen}
          onClose={() => setTerminalOpen(false)}
          containerId={container.Id}
          containerName={name}
        />
        {/* Metrics area: full width */}
        <Box sx={{ mt: 2, width: "100%" }}>
          {loading ? (
            <Box sx={{ display: "flex", justifyContent: "center", mt: 2 }}>
              <ComponentLoader />
            </Box>
          ) : (
            <>
              <MetricBar
                label="CPU"
                percent={cpuPercent}
                color={theme.palette.primary.main}
                tooltip="CPU Usage"
                rightLabel={`${cpuPercent.toFixed(1)}%`}
              />
              <MetricBar
                label="MEM"
                percent={memPercent}
                color={theme.palette.primary.main}
                tooltip={`Memory Usage: ${formatBytes(memUsage)} / ${formatBytes(memLimit)}`}
                rightLabel={formatBytes(memUsage)}
              />
            </>
          )}
        </Box>
      </FrostedCard>
    </Grid>
  );
};

export default ContainerCard;
