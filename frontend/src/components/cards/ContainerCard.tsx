import { Box, Chip, Collapse, Divider, Tooltip, Typography, Fade } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import React, { useMemo, useState, useCallback, useEffect } from "react";
import { toast } from "sonner";

import ActionButton from "../../pages/main/docker/ActionButton";
import LogsDialog from "../../pages/main/docker/LogsDialog";
import ComponentLoader from "../loaders/ComponentLoader";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import DockerIcon from "@/components/docker/DockerIcon";
import MetricBar from "@/components/gauge/MetricBar";
import TerminalDialog from "@/pages/main/docker/TerminalDialog";
import { ContainerInfo } from "@/types/container";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

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

  // expand / collapse
  const [expanded, setExpanded] = useState(false);
  const ports = useMemo(() => {
    const seen = new Set<string>();
    return (container.Ports ?? []).filter((p) => {
      const key = p.PublicPort
        ? `${p.PublicPort}:${p.PrivatePort}/${p.Type}`
        : `${p.PrivatePort}/${p.Type}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  }, [container.Ports]);
  const hasPorts = ports.length > 0;

  useEffect(() => {
    if (!expanded) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") setExpanded(false);
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [expanded]);

  // derived
  const name = useMemo(
    () => container.Names?.[0]?.replace("/", "") || "Unnamed",
    [container.Names],
  );

  // ---- actions (start/stop/restart/remove) ----
  const { mutate: startContainer, isPending: isStartPending } =
    linuxio.docker.start_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} started successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, `Failed to start container ${name}`),
        );
      },
    });

  const { mutate: stopContainer, isPending: isStopPending } =
    linuxio.docker.stop_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} stopped successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, `Failed to stop container ${name}`),
        );
      },
    });

  const { mutate: restartContainer, isPending: isRestartPending } =
    linuxio.docker.restart_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} restarted successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, `Failed to restart container ${name}`),
        );
      },
    });

  const { mutate: removeContainer, isPending: isRemovePending } =
    linuxio.docker.remove_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} removed successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, `Failed to remove container ${name}`),
        );
      },
    });

  const isActionPending =
    isStartPending || isStopPending || isRestartPending || isRemovePending;

  const handleAction = useCallback(
    (action: "start" | "stop" | "restart" | "remove") => {
      const containerId = [container.Id];
      switch (action) {
        case "start":
          startContainer(containerId);
          break;
        case "stop":
          stopContainer(containerId);
          break;
        case "restart":
          restartContainer(containerId);
          break;
        case "remove":
          removeContainer(containerId);
          break;
      }
    },
    [
      container.Id,
      startContainer,
      stopContainer,
      restartContainer,
      removeContainer,
    ],
  );

  const handleLogsClick = () => setLogDialogOpen(true);

  // ---- metrics ----
  const cpuPercent = container.metrics?.cpu_percent ?? 0;
  const memUsage = container.metrics?.mem_usage ?? 0;
  const memLimit = container.metrics?.mem_limit ?? 0;
  const memPercent =
    memLimit > 0 ? Math.min((memUsage / memLimit) * 100, 100) : 0;

  return (
    <FrostedCard
      onClick={hasPorts ? () => setExpanded((v) => !v) : undefined}
      onMouseDown={hasPorts ? (e) => e.preventDefault() : undefined}
      sx={{
        p: 2,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        position: "relative",
        cursor: hasPorts ? "pointer" : "default",
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
          sx={{
            width: 48,
            height: 48,
            minWidth: 48,
            minHeight: 48,
            flexShrink: 0,
            mr: 1.5,
            alignSelf: "flex-start",
          }}
        >
          <DockerIcon identifier={container.icon} size={48} alt={name} />
        </Box>
        <Box sx={{ flex: 0.95, minWidth: 0 }}>
          <Typography
            variant="subtitle1"
            fontWeight="600"
            noWrap
            sx={{ ml: 1, mr: 0.1, mb: 0.5, fontSize: "1.05rem" }}
          >
            {name}
          </Typography>
          <Box sx={{ display: "flex", gap: 0.5 }}>
            {container.State !== "running" && (
              <Tooltip title="Start Container" arrow>
                <span onClick={(e) => e.stopPropagation()}>
                  <ActionButton
                    icon="mdi:play"
                    onClick={() => handleAction("start")}
                  />
                </span>
              </Tooltip>
            )}
            {container.State === "running" && (
              <Tooltip title="Stop Container" arrow>
                <span onClick={(e) => e.stopPropagation()}>
                  <ActionButton
                    icon="mdi:stop"
                    onClick={() => handleAction("stop")}
                  />
                </span>
              </Tooltip>
            )}
            <Tooltip title="Restart Container" arrow>
              <span onClick={(e) => e.stopPropagation()}>
                <ActionButton
                  icon="mdi:restart"
                  onClick={() => handleAction("restart")}
                />
              </span>
            </Tooltip>
            <Tooltip title="Remove Container" arrow>
              <span onClick={(e) => e.stopPropagation()}>
                <ActionButton
                  icon="mdi:delete"
                  onClick={() => handleAction("remove")}
                />
              </span>
            </Tooltip>
            <Tooltip title="View Logs" arrow>
              <span onClick={(e) => e.stopPropagation()}>
                <ActionButton
                  icon="mdi:file-document-outline"
                  onClick={handleLogsClick}
                />
              </span>
            </Tooltip>
            <Tooltip title="Open Terminal" arrow>
              <span onClick={(e) => e.stopPropagation()}>
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
        containerName={name}
        containerId={container.Id}
      />

      <TerminalDialog
        open={terminalOpen}
        onClose={() => setTerminalOpen(false)}
        containerId={container.Id}
        containerName={name}
      />

      {/* Metrics area: full width */}
      <Box sx={{ mt: 2, width: "100%" }}>
        {isActionPending ? (
          <ComponentLoader />
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
              tooltip={`Memory Usage: ${formatFileSize(memUsage)} / ${formatFileSize(memLimit)}`}
              rightLabel={formatFileSize(memUsage)}
            />
          </>
        )}
      </Box>

      {/* Ports section */}
      <Collapse in={expanded} timeout={250} unmountOnExit>
        <Divider sx={{ mt: 1, mb: 1.5 }} />
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
          {ports.map((p, i) => {
            const label =
              p.PublicPort
                ? `${p.PublicPort}:${p.PrivatePort}/${p.Type}`
                : `${p.PrivatePort}/${p.Type}`;
            return (
              <Chip
                key={i}
                label={label}
                size="small"
                sx={{ fontFamily: "monospace", fontSize: "0.7rem", height: 22 }}
              />
            );
          })}
        </Box>
      </Collapse>
    </FrostedCard>
  );
};

export default ContainerCard;
