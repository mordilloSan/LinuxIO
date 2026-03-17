import { Collapse, Switch } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import React, {
  Suspense,
  useMemo,
  useState,
  useCallback,
  useEffect,
} from "react";
import { toast } from "sonner";

import ActionButton from "../../pages/main/docker/ActionButton";
import ComponentLoader from "../loaders/ComponentLoader";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import DockerIcon from "@/components/docker/DockerIcon";
import MetricBar from "@/components/gauge/MetricBar";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { ContainerInfo } from "@/types/container";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

const LogsDialog = React.lazy(() => import("@/pages/main/docker/LogsDialog"));
const TerminalDialog = React.lazy(
  () => import("@/pages/main/docker/TerminalDialog"),
);

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

/** Resolve a MUI palette path like "success.main" to an actual color string. */
const resolveColor = (palette: any, path: string): string => {
  const [group, key] = path.split(".") as [string, string];
  return palette[group]?.[key];
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
  const [hasLoadedLogsDialog, setHasLoadedLogsDialog] = useState(false);
  const [hasLoadedTerminalDialog, setHasLoadedTerminalDialog] = useState(false);

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

  const handleLogsClick = () => {
    setHasLoadedLogsDialog(true);
    setLogDialogOpen(true);
  };

  const handleTerminalClick = () => {
    setHasLoadedTerminalDialog(true);
    setTerminalOpen(true);
  };

  // ---- auto-update ----
  const isWatchtowerContainer =
    container.Labels?.["com.docker.compose.project"] === "linuxio-watchtower";

  const { data: autoUpdateContainers = [] } =
    linuxio.docker.list_auto_update_containers.useQuery({
      enabled: !isWatchtowerContainer,
    });
  const autoUpdate = autoUpdateContainers.includes(name);
  const [autoUpdateLoading, setAutoUpdateLoading] = useState(false);
  const autoUpdateChecked = isWatchtowerContainer ? true : autoUpdate;
  const autoUpdateDisabled = autoUpdateLoading || isWatchtowerContainer;
  const autoUpdateTooltip = isWatchtowerContainer
    ? "Auto Update: Managed by LinuxIO"
    : autoUpdate
      ? "Auto Update: On"
      : "Auto Update: Off";

  const handleAutoUpdateToggle = useCallback(
    async (enabled: boolean) => {
      if (isWatchtowerContainer) return;
      setAutoUpdateLoading(true);
      try {
        await linuxio.docker.set_auto_update.call(
          JSON.stringify({ container: name, enabled }),
        );
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_auto_update_containers.queryKey(),
        });
        toast.success(
          `Auto-update ${enabled ? "enabled" : "disabled"} for ${name}`,
        );
      } catch {
        toast.error(`Failed to update auto-update setting for ${name}`);
      } finally {
        setAutoUpdateLoading(false);
      }
    },
    [isWatchtowerContainer, name, queryClient],
  );

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
      hoverLift={hasPorts}
      style={{
        padding: 8,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        position: "relative",
        cursor: hasPorts ? "pointer" : "default",
      }}
    >
      {/* Status dot */}
      <AppTooltip title={getStatusTooltip(container)} placement="top" arrow>
        <div
          style={{
            position: "absolute",
            top: 18,
            right: 8,
            width: 10,
            height: 10,
            borderRadius: "50%",
            backgroundColor: resolveColor(
              theme.palette,
              getStatusColor(container),
            ),
            cursor: "default",
          }}
        />
      </AppTooltip>

      {/* Top row: Icon + Name + Buttons */}
      <div
        style={{
          display: "flex",
          flexDirection: "row",
          alignItems: "center",
          width: "100%",
        }}
      >
        <div
          style={{
            width: 48,
            height: 48,
            minWidth: 48,
            minHeight: 48,
            flexShrink: 0,
            marginRight: 6,
            alignSelf: "flex-start",
          }}
        >
          <DockerIcon identifier={container.icon} size={48} alt={name} />
        </div>
        <div style={{ flex: 0.95, minWidth: 0 }}>
          <AppTypography
            variant="subtitle1"
            fontWeight={600}
            noWrap
            style={{
              marginLeft: 4,
              marginRight: 0.4,
              marginBottom: 2,
              fontSize: "1.05rem",
            }}
          >
            {name}
          </AppTypography>
          <div style={{ display: "flex", gap: 2, alignItems: "center" }}>
            {container.Labels?.["com.docker.compose.project"] ===
            "linuxio-watchtower" ? (
              <AppTooltip title="View Logs" arrow>
                <Chip
                  label="Managed by LinuxIO"
                  size="small"
                  variant="soft"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleLogsClick();
                  }}
                  sx={{
                    fontSize: "0.68rem",
                    opacity: 0.7,
                    cursor: "pointer",
                    "&:hover": { opacity: 1 },
                  }}
                />
              </AppTooltip>
            ) : (
              <>
                {container.State !== "running" && (
                  <AppTooltip title="Start Container" arrow>
                    <span onClick={(e) => e.stopPropagation()}>
                      <ActionButton
                        icon="mdi:play"
                        onClick={() => handleAction("start")}
                      />
                    </span>
                  </AppTooltip>
                )}
                {container.State === "running" && (
                  <AppTooltip title="Stop Container" arrow>
                    <span onClick={(e) => e.stopPropagation()}>
                      <ActionButton
                        icon="mdi:stop"
                        onClick={() => handleAction("stop")}
                      />
                    </span>
                  </AppTooltip>
                )}
                <AppTooltip title="Restart Container" arrow>
                  <span onClick={(e) => e.stopPropagation()}>
                    <ActionButton
                      icon="mdi:restart"
                      onClick={() => handleAction("restart")}
                    />
                  </span>
                </AppTooltip>
                <AppTooltip title="Remove Container" arrow>
                  <span onClick={(e) => e.stopPropagation()}>
                    <ActionButton
                      icon="mdi:delete"
                      onClick={() => handleAction("remove")}
                    />
                  </span>
                </AppTooltip>
                <AppTooltip title="View Logs" arrow>
                  <span onClick={(e) => e.stopPropagation()}>
                    <ActionButton
                      icon="mdi:file-document-outline"
                      onClick={handleLogsClick}
                    />
                  </span>
                </AppTooltip>
              </>
            )}
            {container.Labels?.["com.docker.compose.project"] !==
              "linuxio-watchtower" && (
              <AppTooltip title="Open Terminal" arrow>
                <span onClick={(e) => e.stopPropagation()}>
                  <ActionButton
                    icon="mdi:console"
                    onClick={handleTerminalClick}
                  />
                </span>
              </AppTooltip>
            )}
            {container.url && (
              <AppTooltip title="Open App" arrow>
                <span onClick={(e) => e.stopPropagation()}>
                  <ActionButton
                    icon="mdi:open-in-new"
                    onClick={() =>
                      window.open(container.url, "_blank", "noopener")
                    }
                  />
                </span>
              </AppTooltip>
            )}
          </div>
        </div>
      </div>

      <Suspense fallback={null}>
        {hasLoadedLogsDialog && (
          <LogsDialog
            open={logDialogOpen}
            onClose={() => setLogDialogOpen(false)}
            containerName={name}
            containerId={container.Id}
          />
        )}

        {hasLoadedTerminalDialog && (
          <TerminalDialog
            open={terminalOpen}
            onClose={() => setTerminalOpen(false)}
            containerId={container.Id}
            containerName={name}
          />
        )}
      </Suspense>

      {/* Metrics area: full width */}
      <div style={{ marginTop: 8, width: "100%" }}>
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
      </div>

      {/* Auto-update toggle */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginTop: 6,
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <AppTypography
          variant="caption"
          color={isWatchtowerContainer ? "text.disabled" : "text.secondary"}
        >
          Auto Update
        </AppTypography>
        <AppTooltip title={autoUpdateTooltip}>
          <span style={{ display: "inline-flex" }}>
            <Switch
              size="small"
              checked={autoUpdateChecked}
              onChange={(e) => handleAutoUpdateToggle(e.target.checked)}
              disabled={autoUpdateDisabled}
              sx={
                isWatchtowerContainer
                  ? {
                      "& .MuiSwitch-switchBase.Mui-checked.Mui-disabled": {
                        color: "action.disabled",
                      },
                      "& .MuiSwitch-switchBase.Mui-disabled + .MuiSwitch-track":
                        {
                          opacity: 1,
                          backgroundColor: "action.disabledBackground",
                        },
                    }
                  : undefined
              }
            />
          </span>
        </AppTooltip>
      </div>

      {/* Ports section */}
      <Collapse in={expanded} timeout={250} unmountOnExit>
        <AppDivider style={{ marginTop: 8, marginBottom: 12 }} />
        <div style={{ display: "flex", flexWrap: "wrap", gap: 3 }}>
          {ports.map((p, i) => {
            const label = p.PublicPort
              ? `${p.PublicPort}:${p.PrivatePort}/${p.Type}`
              : `${p.PrivatePort}/${p.Type}`;
            return (
              <Chip
                key={i}
                label={label}
                size="small"
                variant="soft"
                sx={{ fontFamily: "monospace", fontSize: "0.7rem", height: 22 }}
              />
            );
          })}
        </div>
      </Collapse>
    </FrostedCard>
  );
};

export default ContainerCard;
