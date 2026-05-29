import { useQueryClient } from "@tanstack/react-query";
import React, {
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";

import ActionButton from "../../pages/main/docker/ActionButton";
import AppCircularProgress from "../ui/AppCircularProgress";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import DockerIcon from "@/components/docker/DockerIcon";
import MetricBar from "@/components/gauge/MetricBar";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { ContainerInfo } from "@/types/container";
import { isLinuxIOManagedContainer } from "@/utils/dockerManaged";
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
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
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
  const isManagedContainer = isLinuxIOManagedContainer(container.Labels);

  const { data: autoUpdateContainers = [] } =
    linuxio.docker.list_auto_update_containers.useQuery({
      enabled: !isManagedContainer,
    });
  const autoUpdate = autoUpdateContainers.includes(name);
  const [autoUpdateLoading, setAutoUpdateLoading] = useState(false);
  const autoUpdateChecked = isManagedContainer ? true : autoUpdate;
  const autoUpdateDisabled = autoUpdateLoading || isManagedContainer;
  const autoUpdateTooltip = isManagedContainer
    ? "Auto Update: Managed by LinuxIO"
    : autoUpdate
      ? "Auto Update: On"
      : "Auto Update: Off";

  const handleAutoUpdateToggle = useCallback(
    async (enabled: boolean) => {
      if (isManagedContainer) return;
      setAutoUpdateLoading(true);
      try {
        await linuxio.docker.set_auto_update.call({ container: name, enabled });
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
    [isManagedContainer, name, queryClient, toast],
  );

  // ---- metrics ----
  const cpuPercent = container.metrics?.cpu_percent ?? 0;
  const memUsage = container.metrics?.mem_usage ?? 0;
  const memLimit = container.metrics?.mem_limit ?? 0;
  const memPercent =
    memLimit > 0 ? Math.min((memUsage / memLimit) * 100, 100) : 0;

  return (
    <FrostedCard
      hoverLift={hasPorts}
      onClick={hasPorts ? () => setExpanded((v) => !v) : undefined}
      onMouseDown={hasPorts ? (e) => e.preventDefault() : undefined}
      style={{
        padding: 8,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        position: "relative",
        cursor: hasPorts ? "pointer" : "default",
      }}
    >
      {/* Loading overlay */}
      {isActionPending && (
        <div
          style={{
            position: "absolute",
            inset: 0,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            borderRadius: "inherit",
            backgroundColor: "var(--app-overlay-dark)",
            zIndex: 1,
          }}
        >
          <AppCircularProgress size={32} />
        </div>
      )}

      {/* Status dot */}
      <StatusDot
        absolute
        color={resolveColor(theme.palette, getStatusColor(container))}
        tooltip={getStatusTooltip(container)}
      />

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
          <DockerIcon alt={name} identifier={container.icon} size={48} />
        </div>
        <div style={{ flex: 0.95, minWidth: 0 }}>
          <AppTypography
            fontWeight={600}
            noWrap
            style={{
              marginLeft: 4,
              marginRight: 0.4,
              marginBottom: 2,
              fontSize: "1.05rem",
            }}
            variant="subtitle1"
          >
            {name}
          </AppTypography>
          <div style={{ display: "flex", gap: 2, alignItems: "center" }}>
            {isManagedContainer ? (
              <AppTooltip arrow title="View Logs">
                <Chip
                  className="chip-interactive"
                  label="Managed by LinuxIO"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleLogsClick();
                  }}
                  size="small"
                  style={{
                    fontSize: "0.68rem",
                  }}
                  variant="soft"
                />
              </AppTooltip>
            ) : (
              <>
                {container.State !== "running" && (
                  <AppTooltip arrow title="Start Container">
                    <span onClick={(e) => e.stopPropagation()}>
                      <ActionButton
                        icon="mdi:play"
                        onClick={() => handleAction("start")}
                      />
                    </span>
                  </AppTooltip>
                )}
                {container.State === "running" && (
                  <AppTooltip arrow title="Stop Container">
                    <span onClick={(e) => e.stopPropagation()}>
                      <ActionButton
                        icon="mdi:stop"
                        onClick={() => handleAction("stop")}
                      />
                    </span>
                  </AppTooltip>
                )}
                <AppTooltip arrow title="Restart Container">
                  <span onClick={(e) => e.stopPropagation()}>
                    <ActionButton
                      icon="mdi:restart"
                      onClick={() => handleAction("restart")}
                    />
                  </span>
                </AppTooltip>
                <AppTooltip arrow title="Remove Container">
                  <span onClick={(e) => e.stopPropagation()}>
                    <ActionButton
                      icon="mdi:delete"
                      onClick={() => handleAction("remove")}
                    />
                  </span>
                </AppTooltip>
                <AppTooltip arrow title="View Logs">
                  <span onClick={(e) => e.stopPropagation()}>
                    <ActionButton
                      icon="mdi:file-document-outline"
                      onClick={handleLogsClick}
                    />
                  </span>
                </AppTooltip>
              </>
            )}
            {!isManagedContainer && (
              <AppTooltip arrow title="Open Terminal">
                <span onClick={(e) => e.stopPropagation()}>
                  <ActionButton
                    icon="mdi:console"
                    onClick={handleTerminalClick}
                  />
                </span>
              </AppTooltip>
            )}
            {container.url && (
              <AppTooltip arrow title="Open App">
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
            containerId={container.Id}
            containerName={name}
            onClose={() => setLogDialogOpen(false)}
            open={logDialogOpen}
          />
        )}

        {hasLoadedTerminalDialog && (
          <TerminalDialog
            containerId={container.Id}
            containerName={name}
            onClose={() => setTerminalOpen(false)}
            open={terminalOpen}
          />
        )}
      </Suspense>

      {/* Metrics area: full width */}
      <div style={{ marginTop: 8, width: "100%" }}>
        <MetricBar
          color={theme.palette.primary.main}
          label="CPU"
          percent={cpuPercent}
          rightLabel={`${cpuPercent.toFixed(1)}%`}
          tooltip="CPU Usage"
        />
        <MetricBar
          color={theme.palette.primary.main}
          label="MEM"
          percent={memPercent}
          rightLabel={formatFileSize(memUsage)}
          tooltip={`Memory Usage: ${formatFileSize(memUsage)} / ${formatFileSize(memLimit)}`}
        />
      </div>

      {/* Auto-update toggle */}
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginTop: 6,
        }}
      >
        <AppTypography
          color={isManagedContainer ? "text.disabled" : "text.secondary"}
          variant="caption"
        >
          Auto Update
        </AppTypography>
        <AppTooltip title={autoUpdateTooltip}>
          <span style={{ display: "inline-flex" }}>
            <AppSwitch
              checked={autoUpdateChecked}
              disabled={autoUpdateDisabled}
              onChange={(e) => handleAutoUpdateToggle(e.target.checked)}
              size="small"
            />
          </span>
        </AppTooltip>
      </div>

      {/* Ports section */}
      <AppCollapse in={expanded} timeout={250} unmountOnExit>
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
                style={{
                  fontFamily: "monospace",
                  fontSize: "0.7rem",
                  height: 22,
                }}
                variant="soft"
              />
            );
          })}
        </div>
      </AppCollapse>
    </FrostedCard>
  );
};

export default ContainerCard;
