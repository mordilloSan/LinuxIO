import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense, useCallback, useMemo, useState } from "react";

import { linuxio, type ContainerInfo } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ContainerActions from "@/components/docker/ContainerActions";
import ContainerInfoSections from "@/components/docker/ContainerInfoSections";
import DockerIcon from "@/components/docker/DockerIcon";
import MetricBar from "@/components/gauge/MetricBar";
import AppButton from "@/components/ui/AppButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getContainerStatusColor } from "@/constants/statusColors";
import { CARD_PADDING_LG } from "@/theme/constants";
import { getContainerDisplayState } from "@/utils/dockerContainer";
import { formatFileSize } from "@/utils/formaters";

const LogsDialog = lazy(() => import("@/components/docker/LogsDialog"));
const TerminalDialog = lazy(() => import("@/components/docker/TerminalDialog"));

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

interface ContainerCardProps {
  actionPending?: boolean;
  /**
   * One "CPU - x% · MEM - y" caption line in place of the two metric bars.
   * For cards inside a stack band, whose chrome would otherwise make their
   * grid row taller than a row of loose cards.
   */
  compactMetrics?: boolean;
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
  compactMetrics = false,
  container,
  onSelect,
  selected,
}: ContainerCardBodyProps) => {
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

  const handleLogsClick = () => {
    setHasLoadedLogsDialog(true);
    setLogDialogOpen(true);
  };

  const handleTerminalClick = () => {
    setHasLoadedTerminalDialog(true);
    setTerminalOpen(true);
  };

  // ---- metrics ----
  const cpuPercent = container.metrics?.cpu_percent;
  const memUsage = container.metrics?.memory_usage_bytes;
  const memLimit = container.metrics?.memory_limit_bytes;
  const memPercent =
    memUsage !== undefined && memLimit !== undefined && memLimit > 0
      ? Math.min((memUsage / memLimit) * 100, 100)
      : 0;

  const metricsStatus = container.metrics?.status ?? "unavailable";
  const metricsStatusLabel =
    metricsStatus === "stale"
      ? "Stale metrics"
      : metricsStatus === "not_running"
        ? "Container not running"
        : "Metrics unavailable";
  const memoryTooltip =
    memUsage === undefined
      ? metricsStatusLabel
      : memLimit === undefined
        ? `Memory Usage: ${formatFileSize(memUsage)} (limit unavailable)`
        : `Memory Usage: ${formatFileSize(memUsage)} / ${formatFileSize(memLimit)}`;
  const metricsStatusAffordance =
    metricsStatus === "available" ? null : (
      <AppTooltip arrow title={metricsStatusLabel}>
        <span
          aria-label={metricsStatusLabel}
          role="status"
          style={{
            alignItems: "center",
            color:
              metricsStatus === "stale"
                ? "var(--app-palette-warning-main)"
                : "var(--app-palette-text-secondary)",
            display: "flex",
          }}
        >
          <Icon
            aria-hidden
            icon={
              metricsStatus === "stale"
                ? "mdi:clock-alert-outline"
                : metricsStatus === "not_running"
                  ? "mdi:pause-circle-outline"
                  : "mdi:chart-timeline-variant-shimmer"
            }
            width={16}
          />
        </span>
      </AppTooltip>
    );

  const statusColor = getContainerStatusColor(
    getContainerDisplayState(container),
  );
  const selectedActions = (
    <ContainerActions
      actionPending={actionPending}
      container={container}
      mode="buttons"
      name={name}
      onOpenLogs={handleLogsClick}
      onOpenTerminal={handleTerminalClick}
    />
  );

  return (
    <>
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
              contain: "inline-size",
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
                        color: "var(--app-palette-warning-main)",
                        display: "flex",
                      }}
                    >
                      <Icon aria-hidden icon="mdi:alert" width={16} />
                    </span>
                  </AppTooltip>
                )}
                {metricsStatusAffordance}
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
              gap: "var(--app-space-4)",
              minWidth: 0,
              contain: "inline-size",
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
                    color: "var(--app-palette-warning-main)",
                    display: "flex",
                  }}
                >
                  <Icon aria-hidden icon="mdi:alert" width={16} />
                </span>
              </AppTooltip>
            )}
            {metricsStatusAffordance}
            <StatusDot
              color={statusColor}
              tooltip={getContainerDisplayState(container)}
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
                  }}
                  title={name}
                  toastMeta={DOCKER_TOAST_META}
                  variant="h5"
                >
                  {name}
                </AppTypography>
              </AppButton>
              <ContainerActions
                actionPending={actionPending}
                container={container}
                name={name}
                onOpenLogs={handleLogsClick}
                onOpenTerminal={handleTerminalClick}
              />
            </div>
          </div>

          {/* Metrics area: full width. Compact trades the two bars for one
              caption line, the tooltip keeping the memory limit detail. */}
          <div style={{ marginTop: 8, width: "100%" }}>
            {compactMetrics ? (
              <AppTooltip title={memoryTooltip}>
                <div
                  style={{
                    display: "flex",
                    justifyContent: "space-between",
                    gap: 8,
                    width: "100%",
                  }}
                >
                  <AppTypography
                    color="text.secondary"
                    component="div"
                    noWrap
                    style={{ fontVariantNumeric: "tabular-nums" }}
                    variant="caption"
                  >
                    <span style={{ color: "var(--app-palette-text-primary)" }}>
                      CPU
                    </span>
                    {" - "}
                    {cpuPercent === undefined
                      ? "—"
                      : `${cpuPercent.toFixed(1)}%`}
                  </AppTypography>
                  <AppTypography
                    color="text.secondary"
                    component="div"
                    noWrap
                    style={{ fontVariantNumeric: "tabular-nums" }}
                    variant="caption"
                  >
                    <span style={{ color: "var(--app-palette-text-primary)" }}>
                      MEM
                    </span>
                    {" - "}
                    {memUsage === undefined ? "—" : formatFileSize(memUsage)}
                  </AppTypography>
                </div>
              </AppTooltip>
            ) : (
              <>
                <MetricBar
                  color="var(--app-palette-primary-main)"
                  label="CPU"
                  percent={cpuPercent ?? 0}
                  rightLabel={
                    cpuPercent === undefined
                      ? "Unavailable"
                      : `${cpuPercent.toFixed(1)}%`
                  }
                  tooltip="CPU Usage"
                />
                <MetricBar
                  color="var(--app-palette-primary-main)"
                  label="MEM"
                  percent={memPercent}
                  rightLabel={
                    memUsage === undefined
                      ? "Unavailable"
                      : formatFileSize(memUsage)
                  }
                  tooltip={memoryTooltip}
                />
              </>
            )}
          </div>
        </>
      )}
    </>
  );
};

const ContainerCard = ({ selected = false, ...props }: ContainerCardProps) => (
  <FrostedCard
    accent
    hoverLift={!selected}
    style={{
      padding: CARD_PADDING_LG,
      display: "flex",
      flexDirection: "column",
      height: "100%",
      width: "100%",
      minWidth: 0,
      position: "relative",
      // Selecting a container isolates it outside the grid, where it can no
      // longer be held to reorder — so the line stands down with the lift.
      ...(selected && { borderBottomColor: "transparent" }),
    }}
  >
    <ContainerCardLive {...props} selected={selected} />
  </FrostedCard>
);

export default ContainerCard;
