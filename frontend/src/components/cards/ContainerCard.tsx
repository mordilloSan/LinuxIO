import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense, useCallback, useMemo, useState } from "react";

import { linuxio, type ContainerInfo, useCallMutation } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ContainerInfoSections from "@/components/docker/ContainerInfoSections";
import DockerIcon from "@/components/docker/DockerIcon";
import { useDockerUpdateOperation } from "@/components/docker/DockerUpdateOperationProvider";
import MetricBar from "@/components/gauge/MetricBar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

import AppCircularProgress from "../ui/AppCircularProgress";

const LogsDialog = lazy(() => import("@/components/docker/LogsDialog"));
const TerminalDialog = lazy(() => import("@/components/docker/TerminalDialog"));

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

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
  actionPending?: boolean;
  containerId: string;
  onSelect?: () => void;
  selected?: boolean;
}

type ContainerCardLiveProps = Omit<ContainerCardProps, "selected"> & {
  selected: boolean;
};

const ContainerCardLive = ({
  containerId,
  ...props
}: ContainerCardLiveProps) => {
  const selectContainer = useCallback(
    (containers: ContainerInfo[]) =>
      containers.find((item) => item.Id === containerId),
    [containerId],
  );
  // Cards consume the list cache by identity. The page owns the sole polling
  // observer; this observer only receives fresh values from that shared cache.
  const { data: container } = useQuery({
    ...linuxio.docker.list_containers,
    refetchOnMount: false,
    select: selectContainer,
  });

  if (!container) return null;

  return <ContainerCardBody {...props} container={container} />;
};

type ContainerCardBodyProps = Omit<ContainerCardLiveProps, "containerId"> & {
  container: ContainerInfo;
};

const ContainerCardBody = ({
  actionPending = false,
  container,
  onSelect,
  selected,
}: ContainerCardBodyProps) => {
  const theme = useAppTheme();
  const { isUpdating, startUpdate, updating } = useDockerUpdateOperation();

  // dialogs
  const [logDialogOpen, setLogDialogOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [hasLoadedLogsDialog, setHasLoadedLogsDialog] = useState(false);
  const [hasLoadedTerminalDialog, setHasLoadedTerminalDialog] = useState(false);

  // derived
  const name = useMemo(
    () => container.Names?.[0]?.replace("/", "") || "Unnamed",
    [container.Names],
  );

  // ---- actions (start/stop/restart/remove) ----
  const { mutate: startContainer, isPending: isStartPending } = useCallMutation(
    linuxio.docker.start_container,
    {
      success: `Container ${name} started successfully`,
      error: `Failed to start container ${name}`,
      toast: DOCKER_TOAST_META,
    },
  );

  const { mutate: stopContainer, isPending: isStopPending } = useCallMutation(
    linuxio.docker.stop_container,
    {
      success: `Container ${name} stopped successfully`,
      error: `Failed to stop container ${name}`,
      toast: DOCKER_TOAST_META,
    },
  );

  const { mutate: restartContainer, isPending: isRestartPending } =
    useCallMutation(linuxio.docker.restart_container, {
      success: `Container ${name} restarted successfully`,
      error: `Failed to restart container ${name}`,
      toast: DOCKER_TOAST_META,
    });

  const { mutate: removeContainer, isPending: isRemovePending } =
    useCallMutation(linuxio.docker.remove_container, {
      success: `Container ${name} removed successfully`,
      error: `Failed to remove container ${name}`,
      toast: DOCKER_TOAST_META,
    });

  const isUpdatePending = isUpdating(container.Id);

  const isActionPending =
    actionPending ||
    isStartPending ||
    isStopPending ||
    isRestartPending ||
    isRemovePending ||
    updating;

  const handleAction = useCallback(
    (action: "start" | "stop" | "restart" | "remove") => {
      const request = { containerId: container.Id };
      switch (action) {
        case "start":
          startContainer(request);
          break;
        case "stop":
          stopContainer(request);
          break;
        case "restart":
          restartContainer(request);
          break;
        case "remove":
          removeContainer(request);
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

  const handleUpdateClick = useCallback(() => {
    startUpdate(container.Id, name);
  }, [container.Id, name, startUpdate]);

  // ---- metrics ----
  const cpuPercent = container.metrics?.cpu_percent ?? 0;
  const memUsage = container.metrics?.mem_usage ?? 0;
  const memLimit = container.metrics?.mem_limit ?? 0;
  const memPercent =
    memLimit > 0 ? Math.min((memUsage / memLimit) * 100, 100) : 0;

  const statusColor = resolveColor(theme.palette, getStatusColor(container));
  // Service-style action buttons, shown in the selected card.
  const selectedActions = (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        flexWrap: "wrap",
        gap: 6,
        marginTop: 12,
        minWidth: 0,
      }}
    >
      {container.State === "running" ? (
        <AppTooltip arrow placement="top" title="Stop Container">
          <span>
            <AppButton
              color="error"
              disabled={isActionPending}
              onClick={() => handleAction("stop")}
              size="small"
              startIcon={<Icon height={16} icon="mdi:stop-circle" width={16} />}
              variant="outlined"
            >
              Stop
            </AppButton>
          </span>
        </AppTooltip>
      ) : (
        <AppTooltip arrow placement="top" title="Start Container">
          <span>
            <AppButton
              color="success"
              disabled={isActionPending}
              onClick={() => handleAction("start")}
              size="small"
              startIcon={<Icon height={16} icon="mdi:play" width={16} />}
              variant="outlined"
            >
              Start
            </AppButton>
          </span>
        </AppTooltip>
      )}
      <AppTooltip arrow placement="top" title="Restart Container">
        <span>
          <AppButton
            disabled={isActionPending}
            onClick={() => handleAction("restart")}
            size="small"
            startIcon={<Icon height={16} icon="mdi:restart" width={16} />}
            variant="outlined"
          >
            Restart
          </AppButton>
        </span>
      </AppTooltip>
      {container.updateAvailable && container.State === "running" && (
        <AppTooltip arrow placement="top" title="Update Container">
          <span>
            <AppButton
              color="warning"
              disabled={isActionPending}
              onClick={handleUpdateClick}
              size="small"
              startIcon={
                isUpdatePending ? (
                  <AppCircularProgress color="inherit" size={14} />
                ) : (
                  <Icon height={16} icon="mdi:update" width={16} />
                )
              }
              variant="outlined"
            >
              {isUpdatePending ? "Updating" : "Update"}
            </AppButton>
          </span>
        </AppTooltip>
      )}
      <AppTooltip arrow placement="top" title="Remove Container">
        <span>
          <AppButton
            color="error"
            disabled={isActionPending}
            onClick={() => handleAction("remove")}
            size="small"
            startIcon={<Icon height={16} icon="mdi:delete" width={16} />}
            variant="outlined"
          >
            Remove
          </AppButton>
        </span>
      </AppTooltip>
      <AppTooltip arrow placement="top" title="Open Terminal">
        <span>
          <AppButton
            disabled={isActionPending}
            onClick={handleTerminalClick}
            size="small"
            startIcon={<Icon height={16} icon="mdi:console" width={16} />}
            variant="outlined"
          >
            Terminal
          </AppButton>
        </span>
      </AppTooltip>
      {container.url && (
        <AppTooltip arrow placement="top" title="Open App">
          <span>
            <AppButton
              onClick={() => window.open(container.url, "_blank", "noopener")}
              size="small"
              startIcon={<Icon height={16} icon="mdi:open-in-new" width={16} />}
              variant="outlined"
            >
              Open
            </AppButton>
          </span>
        </AppTooltip>
      )}
    </div>
  );

  return (
    <>
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

      {/* Lazy dialogs (logs / terminal) */}
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

      {selected ? (
        <>
          <AppButton
            aria-controls={`container-card-${container.Id}`}
            aria-expanded={selected}
            aria-label={`Collapse ${name}`}
            color="inherit"
            fullWidth
            onClick={onSelect}
            style={{
              alignItems: "stretch",
              color: "inherit",
              flexDirection: "column",
              justifyContent: "flex-start",
              padding: 0,
              textAlign: "left",
            }}
          >
            {/* Header: icon + title/subtitle + status dot (matches service card) */}
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "flex-start",
                marginBottom: 12,
                gap: 8,
              }}
            >
              <div
                style={{
                  flex: 1,
                  minWidth: 0,
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                }}
              >
                <div style={{ width: 36, height: 36, flexShrink: 0 }}>
                  <DockerIcon
                    alt={name}
                    identifier={container.icon}
                    size={36}
                  />
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <AppTypography
                    component="div"
                    fontSize="0.875rem"
                    fontWeight="bold"
                    noWrap
                    title={name}
                    toastMeta={DOCKER_TOAST_META}
                    variant="body2"
                  >
                    {name}
                  </AppTypography>
                  <AppTypography
                    color="text.secondary"
                    component="div"
                    fontSize="0.7rem"
                    noWrap
                    style={{ marginTop: 2 }}
                    title={container.Image}
                    variant="caption"
                  >
                    {container.Image}
                  </AppTypography>
                </div>
              </div>
              <div
                style={{
                  alignItems: "center",
                  display: "flex",
                  gap: 4,
                  marginTop: 4,
                }}
              >
                {container.updateAvailable && (
                  <AppTooltip arrow title="Update available">
                    <span
                      aria-label="Update available"
                      role="img"
                      style={{
                        alignItems: "center",
                        color: theme.palette.warning.main,
                        display: "flex",
                      }}
                    >
                      <Icon aria-hidden icon="mdi:alert" width={16} />
                    </span>
                  </AppTooltip>
                )}
                <StatusDot color={statusColor} size={8} />
              </div>
            </div>
          </AppButton>

          {/* Body: config sections (fills) + actions pinned to the bottom */}
          <div
            id={`container-card-${container.Id}`}
            style={{
              flex: 1,
              display: "flex",
              flexDirection: "column",
              gap: theme.spacing(1.25),
              minWidth: 0,
            }}
          >
            <ContainerInfoSections
              container={container}
              sections={["overview", "networks"]}
            />
          </div>
          {selectedActions}
        </>
      ) : (
        <>
          <div
            style={{
              alignItems: "center",
              display: "flex",
              gap: 4,
              position: "absolute",
              right: 8,
              top: 14,
            }}
          >
            {container.updateAvailable && (
              <AppTooltip arrow title="Update available">
                <span
                  aria-label="Update available"
                  role="img"
                  style={{
                    alignItems: "center",
                    color: theme.palette.warning.main,
                    display: "flex",
                  }}
                >
                  <Icon aria-hidden icon="mdi:alert" width={16} />
                </span>
              </AppTooltip>
            )}
            <StatusDot
              color={statusColor}
              tooltip={getStatusTooltip(container)}
            />
          </div>

          {/* Top row: Icon + Name + action icons */}
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
              <AppButton
                aria-label={`Select ${name}`}
                color="inherit"
                fullWidth
                onClick={onSelect}
                style={{
                  alignItems: "flex-start",
                  color: "inherit",
                  justifyContent: "flex-start",
                  minWidth: 0,
                  padding: 0,
                  textAlign: "left",
                }}
              >
                <AppTypography
                  fontWeight={600}
                  noWrap
                  style={{
                    marginLeft: 4,
                    marginRight: 0.4,
                    marginBottom: 2,
                    fontSize: "1.05rem",
                  }}
                  title={name}
                  toastMeta={DOCKER_TOAST_META}
                  variant="subtitle1"
                >
                  {name}
                </AppTypography>
              </AppButton>
              <div style={{ display: "flex", gap: 2, alignItems: "center" }}>
                {container.State !== "running" && (
                  <AppTooltip arrow title="Start Container">
                    <span>
                      <AppActionIconButton
                        icon="mdi:play"
                        iconSize={16}
                        label="Start Container"
                        onClick={() => handleAction("start")}
                        tooltip={false}
                      />
                    </span>
                  </AppTooltip>
                )}
                {container.State === "running" && (
                  <AppTooltip arrow title="Stop Container">
                    <span>
                      <AppActionIconButton
                        icon="mdi:stop"
                        iconSize={16}
                        label="Stop Container"
                        onClick={() => handleAction("stop")}
                        tooltip={false}
                      />
                    </span>
                  </AppTooltip>
                )}
                <AppTooltip arrow title="Restart Container">
                  <span>
                    <AppActionIconButton
                      icon="mdi:restart"
                      iconSize={16}
                      label="Restart Container"
                      onClick={() => handleAction("restart")}
                      tooltip={false}
                    />
                  </span>
                </AppTooltip>
                {container.updateAvailable && container.State === "running" && (
                  <AppTooltip arrow title="Update Container">
                    <span>
                      <AppActionIconButton
                        icon="mdi:update"
                        iconSize={16}
                        label="Update Container"
                        loading={isUpdatePending}
                        onClick={handleUpdateClick}
                        tooltip={false}
                      />
                    </span>
                  </AppTooltip>
                )}
                <AppTooltip arrow title="Remove Container">
                  <span>
                    <AppActionIconButton
                      icon="mdi:delete"
                      iconSize={16}
                      label="Remove Container"
                      onClick={() => handleAction("remove")}
                      tooltip={false}
                    />
                  </span>
                </AppTooltip>
                <AppTooltip arrow title="View Logs">
                  <span>
                    <AppActionIconButton
                      icon="mdi:file-document-outline"
                      iconSize={16}
                      label="View Logs"
                      onClick={handleLogsClick}
                      tooltip={false}
                    />
                  </span>
                </AppTooltip>
                <AppTooltip arrow title="Open Terminal">
                  <span>
                    <AppActionIconButton
                      icon="mdi:console"
                      iconSize={16}
                      label="Open Terminal"
                      onClick={handleTerminalClick}
                      tooltip={false}
                    />
                  </span>
                </AppTooltip>
                {container.url && (
                  <AppTooltip arrow title="Open App">
                    <span>
                      <AppActionIconButton
                        icon="mdi:open-in-new"
                        iconSize={16}
                        label="Open App"
                        onClick={() =>
                          window.open(container.url, "_blank", "noopener")
                        }
                        tooltip={false}
                      />
                    </span>
                  </AppTooltip>
                )}
              </div>
            </div>
          </div>

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
        </>
      )}
    </>
  );
};

const ContainerCard = ({ selected = false, ...props }: ContainerCardProps) => (
  <FrostedCard
    hoverLift={!selected}
    style={{
      padding: 12,
      display: "flex",
      flexDirection: "column",
      height: "100%",
      width: "100%",
      minWidth: 0,
      position: "relative",
      border: "none",
    }}
  >
    <ContainerCardLive {...props} selected={selected} />
  </FrostedCard>
);

export default ContainerCard;
