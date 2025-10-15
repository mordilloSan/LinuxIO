import { Box, Grid, Tooltip, Typography, Fade } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import React, { useMemo, useState } from "react";

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
  const theme = useTheme();
  const queryClient = useQueryClient();

  // dialogs
  const [logDialogOpen, setLogDialogOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);

  // derived
  const name = useMemo(
    () => container.Names?.[0]?.replace("/", "") || "Unnamed",
    [container.Names],
  );
  const iconUrl = useMemo(() => getContainerIconUrl(name), [name]);

  // ---- actions (start/stop/restart/remove) ----
  const actionMutation = useMutation({
    mutationFn: async (
      action: "start" | "stop" | "restart" | "remove" | "exec",
    ) => {
      await axios.post(`/docker/containers/${container.Id}/${action}`);
    },
    onSuccess: () => {
      // refresh list + logs
      queryClient.invalidateQueries({ queryKey: ["containers"] });
      queryClient.invalidateQueries({
        queryKey: ["containerLogs", container.Id],
      });
    },
  });

  const handleAction = (
    action: "start" | "stop" | "restart" | "remove" | "exec",
  ) => actionMutation.mutate(action);

  // ---- logs via react-query (fetch only when dialog is open) ----
  const fetchLogs = async (): Promise<string> => {
    const res = await axios.get(`/docker/containers/${container.Id}/logs`);
    return typeof res.data === "string"
      ? res.data
      : (res.data?.output ?? JSON.stringify(res.data, null, 2));
  };

  const logsQuery = useQuery({
    queryKey: ["containerLogs", container.Id],
    queryFn: fetchLogs,
    enabled: logDialogOpen, // only when dialog is open
    refetchInterval: logDialogOpen ? 4000 : false, // auto-refresh while open
    refetchOnWindowFocus: false,
    retry: 1,
  });

  const prefetchLogs = () =>
    queryClient.prefetchQuery({
      queryKey: ["containerLogs", container.Id],
      queryFn: fetchLogs,
      staleTime: 10_000,
    });

  const handleLogsClick = () => setLogDialogOpen(true);

  // ---- metrics ----
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
                      onClick={() => handleAction("start")}
                    />
                  </span>
                </Tooltip>
              )}
              {container.State === "running" && (
                <Tooltip title="Stop Container" arrow>
                  <span>
                    <ActionButton
                      icon="mdi:stop"
                      onClick={() => handleAction("stop")}
                    />
                  </span>
                </Tooltip>
              )}
              <Tooltip title="Restart Container" arrow>
                <span>
                  <ActionButton
                    icon="mdi:restart"
                    onClick={() => handleAction("restart")}
                  />
                </span>
              </Tooltip>
              <Tooltip title="Remove Container" arrow>
                <span>
                  <ActionButton
                    icon="mdi:delete"
                    onClick={() => handleAction("remove")}
                  />
                </span>
              </Tooltip>
              <Tooltip title="View Logs" arrow>
                <span onMouseEnter={prefetchLogs}>
                  <ActionButton
                    icon="mdi:file-document-outline"
                    onClick={handleLogsClick}
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
          logs={logsQuery.data ?? null}
          loading={logsQuery.isLoading}
          error={
            logsQuery.isError
              ? "Failed to load logs. (Check backend logs for details.)"
              : null
          }
          containerName={name}
          onRefresh={() =>
            queryClient.invalidateQueries({
              queryKey: ["containerLogs", container.Id],
            })
          }
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
          {actionMutation.isPending ? (
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
