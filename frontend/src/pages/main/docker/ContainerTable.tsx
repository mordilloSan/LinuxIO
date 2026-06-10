import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { Suspense, useMemo, useState } from "react";

import ActionButton from "./ActionButton";

import { linuxio } from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSwitch from "@/components/ui/AppSwitch";
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableContainer,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getContainerStatusColor } from "@/constants/statusColors";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { ContainerInfo } from "@/types/container";
import { alpha } from "@/utils/color";
import { isLinuxIOManagedContainer } from "@/utils/dockerManaged";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

const LogsDialog = React.lazy(() => import("@/pages/main/docker/LogsDialog"));
const TerminalDialog = React.lazy(
  () => import("@/pages/main/docker/TerminalDialog"),
);

const DOCKER_TOAST_META = { href: "/docker", label: "Open Docker" };

// ── Helpers ───────────────────────────────────────────────────────────────────

const getDisplayState = (container: ContainerInfo) => {
  const s = container.Status.toLowerCase();
  if (s.includes("unhealthy")) return "Unhealthy";
  if (s.includes("healthy")) return "Healthy";
  if (container.State === "running") return "Running";
  if (container.State === "exited") return "Stopped";
  if (container.State === "dead") return "Dead";
  return container.State;
};

const getStatusDotColor = (state: string) => {
  return getContainerStatusColor(state);
};

const getImageVersion = (image: string) => {
  const noDigest = image.split("@")[0];
  const parts = noDigest.split(":");
  if (parts.length < 2) return "—";
  const tag = parts[parts.length - 1];
  return tag || "—";
};

const formatUptime = (createdUnix: number) => {
  const secs = Math.floor(Date.now() / 1000) - createdUnix;
  if (secs < 0) return "—";
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60) % 60;
  const h = Math.floor(secs / 3600) % 24;
  const d = Math.floor(secs / 86400);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
};

// ── Per-row component ─────────────────────────────────────────────────────────

interface ContainerRowProps {
  container: ContainerInfo;
  editMode?: boolean;
  index: number;
}

const ContainerRow: React.FC<ContainerRowProps> = ({
  container,
  index,
  editMode,
}) => {
  const theme = useAppTheme();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: container.Id });
  const [logDialogOpen, setLogDialogOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [hasLoadedLogs, setHasLoadedLogs] = useState(false);
  const [hasLoadedTerminal, setHasLoadedTerminal] = useState(false);

  const name = useMemo(
    () => container.Names?.[0]?.replace("/", "") || "Unnamed",
    [container.Names],
  );

  const { mutate: startContainer } = linuxio.docker.start_container.useMutation(
    {
      onSuccess: () => {
        toast.success(`Container ${name} started`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to start ${name}`)),
    },
  );
  const { mutate: stopContainer } = linuxio.docker.stop_container.useMutation({
    onSuccess: () => {
      toast.success(`Container ${name} stopped`);
      queryClient.invalidateQueries({
        queryKey: linuxio.docker.list_containers.queryKey(),
      });
    },
    onError: (err: Error) =>
      toast.error(getMutationErrorMessage(err, `Failed to stop ${name}`)),
  });
  const { mutate: restartContainer } =
    linuxio.docker.restart_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} restarted`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to restart ${name}`)),
    });
  const { mutate: removeContainer } =
    linuxio.docker.remove_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} removed`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to remove ${name}`)),
    });

  // ── derived ─────────────────────────────────────────────────────────────────
  const cpuPercent = container.metrics?.cpu_percent ?? 0;
  const memUsage = container.metrics?.mem_usage ?? 0;
  const displayState = getDisplayState(container);
  const version = getImageVersion(container.Image);
  const uptime = formatUptime(container.Created);

  // Deduped ports
  const ports = useMemo(() => {
    const seen = new Set<string>();
    return (container.Ports ?? [])
      .filter((p) => {
        // Dedupe by private+public port only — collapses IPv4/IPv6 duplicate entries
        const key = p.PublicPort
          ? `${p.PrivatePort}/${p.Type}:${p.PublicPort}`
          : `${p.PrivatePort}/${p.Type}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      })
      .sort(
        (a, b) => a.PrivatePort - b.PrivatePort || a.Type.localeCompare(b.Type),
      );
  }, [container.Ports]);

  // Networks
  const networks = useMemo(
    () => Object.entries(container.NetworkSettings?.Networks ?? {}),
    [container.NetworkSettings],
  );
  const networkNamesText = networks
    .map(([networkName]) => networkName)
    .join(", ");
  const networkAddressesText = networks
    .map(
      ([networkName, endpoint]) =>
        `${networkName}: ${endpoint.IPAddress || "—"}`,
    )
    .join("\n");

  // Volumes
  const mounts = useMemo(
    () =>
      (container.Mounts ?? []).filter(
        (m) => m.Type === "bind" || m.Type === "volume",
      ),
    [container.Mounts],
  );

  const isManagedContainer = isLinuxIOManagedContainer(container.Labels);

  // ---- auto-update ----
  const { data: rawAutoUpdateContainers } =
    linuxio.docker.list_auto_update_containers.useQuery({
      enabled: !isManagedContainer,
    });
  const autoUpdateContainers = rawAutoUpdateContainers ?? [];
  const autoUpdate = autoUpdateContainers.includes(name);
  const [autoUpdateLoading, setAutoUpdateLoading] = useState(false);
  const autoUpdateChecked = isManagedContainer ? true : autoUpdate;
  const autoUpdateDisabled = autoUpdateLoading || isManagedContainer;
  const autoUpdateTooltip = isManagedContainer
    ? "Auto Update: Managed by LinuxIO"
    : autoUpdate
      ? "Auto Update: On"
      : "Auto Update: Off";

  const handleAutoUpdateToggle = async (enabled: boolean) => {
    if (isManagedContainer) return;
    setAutoUpdateLoading(true);
    try {
      await linuxio.docker.set_auto_update({ container: name, enabled });
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
  };

  const rowBg =
    index % 2 === 0
      ? "transparent"
      : alpha(
          theme.palette.text.primary,
          theme.palette.mode === "dark" ? 0.04 : 0.05,
        );

  return (
    <React.Fragment>
      <AppTableRow
        ref={setNodeRef}
        style={{
          transform: CSS.Transform.toString(transform),
          transition,
          opacity: isDragging ? 0.4 : 1,
          backgroundColor: rowBg,
        }}
      >
        {/* Drag handle */}
        {editMode && (
          <AppTableCell style={{ padding: "0 4px" }} width="28px">
            <span
              {...attributes}
              {...listeners}
              className="drag-handle"
              style={{
                display: "flex",
                alignItems: "center",
                color: theme.palette.text.disabled,
                cursor: "grab",
              }}
            >
              <Icon height={20} icon="mdi:drag" width={20} />
            </span>
          </AppTableCell>
        )}
        {/* Name (with status dot) */}
        <AppTableCell>
          <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
            <StatusDot
              color={getStatusDotColor(displayState)}
              size={8}
              tooltip={displayState}
            />
            <DockerIcon alt={name} identifier={container.icon} size={24} />
            <AppTypography
              copyText={name}
              fontWeight={700}
              noWrap
              title={name}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {name}
            </AppTypography>
          </div>
        </AppTableCell>

        {/* Version */}
        <AppTableCell className="app-table-hide-below-md">
          <AppTypography
            color="text.secondary"
            style={{
              fontFamily: "monospace",
              fontSize: "0.78rem",
            }}
            variant="body2"
          >
            {version}
          </AppTypography>
        </AppTableCell>

        {/* Uptime */}
        <AppTableCell className="app-table-hide-below-md">
          <AppTypography
            color="text.secondary"
            style={{
              fontFamily: "monospace",
              fontSize: "0.78rem",
              fontVariantNumeric: "tabular-nums",
            }}
            variant="body2"
          >
            {uptime}
          </AppTypography>
        </AppTableCell>

        {/* Network */}
        <AppTableCell className="app-table-hide-below-lg">
          {networks.length > 0 ? (
            <AppTypography
              color="text.secondary"
              copyText={networkNamesText}
              noWrap
              style={{
                fontFamily: "monospace",
                fontSize: "0.78rem",
              }}
              title={networkNamesText}
              toastMeta={DOCKER_TOAST_META}
              tooltipOnlyWhenTruncated={networks.length === 1}
              variant="body2"
            >
              {networks[0][0]}
              {networks.length > 1 && (
                <span
                  style={{
                    marginLeft: 2,
                    color: theme.palette.text.disabled,
                  }}
                >
                  +{networks.length - 1}
                </span>
              )}
            </AppTypography>
          ) : (
            <AppTypography color="text.disabled" variant="body2">
              —
            </AppTypography>
          )}
        </AppTableCell>

        {/* Container IP */}
        <AppTableCell className="app-table-hide-below-lg">
          {networks.length > 0 && networks[0][1].IPAddress ? (
            <AppTypography
              copyText={networkAddressesText}
              noWrap
              style={{ fontFamily: "monospace", fontSize: "0.78rem" }}
              title={networkAddressesText}
              toastMeta={DOCKER_TOAST_META}
              tooltipOnlyWhenTruncated={networks.length === 1}
              variant="body2"
            >
              {networks[0][1].IPAddress}
            </AppTypography>
          ) : (
            <AppTypography color="text.disabled" variant="body2">
              —
            </AppTypography>
          )}
        </AppTableCell>

        {/* Ports (Container → Host) */}
        <AppTableCell className="app-table-hide-below-xl">
          {ports.length > 0 ? (
            <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
              {ports.slice(0, 2).map((p, i) => {
                const text = `${p.PrivatePort}/${p.Type} → ${p.PublicPort ?? "—"}`;
                return (
                  <AppTypography
                    copyText={text}
                    key={i}
                    noWrap
                    style={{
                      fontFamily: "monospace",
                      fontSize: "0.75rem",
                    }}
                    title={text}
                    toastMeta={DOCKER_TOAST_META}
                    variant="body2"
                  >
                    <span style={{ color: theme.palette.text.primary }}>
                      {p.PrivatePort}/{p.Type}
                    </span>
                    <span
                      style={{
                        color: theme.palette.text.disabled,
                        marginInline: 2,
                      }}
                    >
                      →
                    </span>
                    <span style={{ color: theme.palette.text.secondary }}>
                      {p.PublicPort ?? "—"}
                    </span>
                  </AppTypography>
                );
              })}
              <AppCollapse in={expanded} timeout={600}>
                <div
                  style={{ display: "flex", flexDirection: "column", gap: 1 }}
                >
                  {ports.length > 2 &&
                    ports.slice(2).map((p, i) => {
                      const text = `${p.PrivatePort}/${p.Type} → ${p.PublicPort ?? "—"}`;
                      return (
                        <AppTypography
                          copyText={text}
                          key={i + 2}
                          noWrap
                          style={{
                            fontFamily: "monospace",
                            fontSize: "0.75rem",
                          }}
                          title={text}
                          toastMeta={DOCKER_TOAST_META}
                          variant="body2"
                        >
                          <span style={{ color: theme.palette.text.primary }}>
                            {p.PrivatePort}/{p.Type}
                          </span>
                          <span
                            style={{
                              color: theme.palette.text.disabled,
                              marginInline: 2,
                            }}
                          >
                            →
                          </span>
                          <span style={{ color: theme.palette.text.secondary }}>
                            {p.PublicPort ?? "—"}
                          </span>
                        </AppTypography>
                      );
                    })}
                </div>
              </AppCollapse>
              {!expanded && ports.length > 2 && (
                <AppTypography color="text.disabled" variant="caption">
                  +{ports.length - 2} more
                </AppTypography>
              )}
            </div>
          ) : (
            <AppTypography color="text.disabled" variant="body2">
              —
            </AppTypography>
          )}
        </AppTableCell>

        {/* Volumes (App → Host) */}
        <AppTableCell
          className="app-table-hide-below-xl"
          style={{ maxWidth: 280 }}
        >
          {mounts.length > 0 ? (
            <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
              {mounts.slice(0, 2).map((m, i) => {
                const text = `${m.Destination} → ${m.Source}`;
                return (
                  <AppTypography
                    copyText={text}
                    key={i}
                    noWrap
                    style={{
                      fontFamily: "monospace",
                      fontSize: "0.75rem",
                    }}
                    title={text}
                    toastMeta={DOCKER_TOAST_META}
                    variant="body2"
                  >
                    <span style={{ color: theme.palette.text.primary }}>
                      {m.Destination}
                    </span>
                    <span
                      style={{
                        color: theme.palette.text.disabled,
                        marginInline: 2,
                      }}
                    >
                      →
                    </span>
                    <span style={{ color: theme.palette.text.secondary }}>
                      {m.Source}
                    </span>
                  </AppTypography>
                );
              })}
              <AppCollapse in={expanded} timeout={600}>
                <div
                  style={{ display: "flex", flexDirection: "column", gap: 1 }}
                >
                  {mounts.length > 2 &&
                    mounts.slice(2).map((m, i) => {
                      const text = `${m.Destination} → ${m.Source}`;
                      return (
                        <AppTypography
                          copyText={text}
                          key={i + 2}
                          noWrap
                          style={{
                            fontFamily: "monospace",
                            fontSize: "0.75rem",
                          }}
                          title={text}
                          toastMeta={DOCKER_TOAST_META}
                          variant="body2"
                        >
                          <span style={{ color: theme.palette.text.primary }}>
                            {m.Destination}
                          </span>
                          <span
                            style={{
                              color: theme.palette.text.disabled,
                              marginInline: 2,
                            }}
                          >
                            →
                          </span>
                          <span style={{ color: theme.palette.text.secondary }}>
                            {m.Source}
                          </span>
                        </AppTypography>
                      );
                    })}
                </div>
              </AppCollapse>
              {!expanded && mounts.length > 2 && (
                <AppTypography color="text.disabled" variant="caption">
                  +{mounts.length - 2} more
                </AppTypography>
              )}
            </div>
          ) : (
            <AppTypography color="text.disabled" variant="body2">
              —
            </AppTypography>
          )}
        </AppTableCell>

        {/* CPU / Memory (stacked) */}
        <AppTableCell
          align="center"
          className="app-table-hide-below-xl"
          width="120px"
        >
          <AppTypography
            color="text.secondary"
            style={{
              fontFamily: "monospace",
              fontSize: "0.78rem",
              fontVariantNumeric: "tabular-nums",
            }}
            variant="body2"
          >
            {cpuPercent.toFixed(1)}%
          </AppTypography>
          <AppTypography
            color="text.secondary"
            style={{
              fontFamily: "monospace",
              fontSize: "0.78rem",
              fontVariantNumeric: "tabular-nums",
            }}
            variant="body2"
          >
            {formatFileSize(memUsage)}
          </AppTypography>
        </AppTableCell>

        {/* Actions + expand */}
        <AppTableCell align="right">
          <div
            style={{
              display: "flex",
              justifyContent: "flex-end",
              alignItems: "center",
              gap: 2,
            }}
          >
            {isManagedContainer ? (
              <AppTooltip title="View Logs">
                <Chip
                  label="Managed by LinuxIO"
                  onClick={() => {
                    setHasLoadedLogs(true);
                    setLogDialogOpen(true);
                  }}
                  size="small"
                  style={{
                    fontSize: "0.68rem",
                    opacity: 0.7,
                    cursor: "pointer",
                  }}
                  variant="soft"
                />
              </AppTooltip>
            ) : (
              <>
                {container.State !== "running" && (
                  <AppTooltip title="Start">
                    <span>
                      <ActionButton
                        icon="mdi:play"
                        onClick={() =>
                          startContainer({ containerId: container.Id })
                        }
                      />
                    </span>
                  </AppTooltip>
                )}
                {container.State === "running" && (
                  <AppTooltip title="Stop">
                    <span>
                      <ActionButton
                        icon="mdi:stop"
                        onClick={() =>
                          stopContainer({ containerId: container.Id })
                        }
                      />
                    </span>
                  </AppTooltip>
                )}
                <AppTooltip title="Restart">
                  <span>
                    <ActionButton
                      icon="mdi:restart"
                      onClick={() =>
                        restartContainer({ containerId: container.Id })
                      }
                    />
                  </span>
                </AppTooltip>
                <AppTooltip title="Remove">
                  <span>
                    <ActionButton
                      icon="mdi:delete"
                      onClick={() =>
                        removeContainer({ containerId: container.Id })
                      }
                    />
                  </span>
                </AppTooltip>
                <AppTooltip title="Logs">
                  <span>
                    <ActionButton
                      icon="mdi:file-document-outline"
                      onClick={() => {
                        setHasLoadedLogs(true);
                        setLogDialogOpen(true);
                      }}
                    />
                  </span>
                </AppTooltip>
              </>
            )}
            {!isManagedContainer && (
              <AppTooltip title="Terminal">
                <span>
                  <ActionButton
                    icon="mdi:console"
                    onClick={() => {
                      setHasLoadedTerminal(true);
                      setTerminalOpen(true);
                    }}
                  />
                </span>
              </AppTooltip>
            )}
            {container.url && (
              <AppTooltip title="Open App">
                <span>
                  <ActionButton
                    icon="mdi:open-in-new"
                    onClick={() =>
                      window.open(container.url, "_blank", "noopener")
                    }
                  />
                </span>
              </AppTooltip>
            )}
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
            <AppIconButton
              className="container-expand-toggle"
              onClick={() => setExpanded((v) => !v)}
              size="small"
              style={{
                marginLeft: 2,
                visibility:
                  ports.length > 2 || mounts.length > 2 ? "visible" : "hidden",
              }}
            >
              <Icon
                height={20}
                icon="mdi:chevron-down"
                style={{
                  transform: expanded ? "rotate(180deg)" : "rotate(0deg)",
                  transition: "0.2s",
                }}
                width={20}
              />
            </AppIconButton>
          </div>
        </AppTableCell>
      </AppTableRow>

      <Suspense fallback={null}>
        {hasLoadedLogs && (
          <LogsDialog
            containerId={container.Id}
            containerName={name}
            onClose={() => setLogDialogOpen(false)}
            open={logDialogOpen}
          />
        )}
        {hasLoadedTerminal && (
          <TerminalDialog
            containerId={container.Id}
            containerName={name}
            onClose={() => setTerminalOpen(false)}
            open={terminalOpen}
          />
        )}
      </Suspense>
    </React.Fragment>
  );
};

// ── Main export ───────────────────────────────────────────────────────────────

interface ContainerTableProps {
  containers: ContainerInfo[];
  editMode?: boolean;
}

const ContainerTable: React.FC<ContainerTableProps> = ({
  containers,
  editMode = false,
}) => {
  const theme = useAppTheme();
  return (
    <div>
      <AppTableContainer className="custom-scrollbar">
        <AppTable>
          <AppTableHead>
            <AppTableRow
              style={{
                backgroundColor: alpha(theme.palette.text.primary, 0.08),
              }}
            >
              {editMode && <AppTableCell width="28px" />}
              <AppTableCell>Name</AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                Version
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                Uptime
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-lg">
                Network
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-lg">
                Container IP
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-xl" width="160px">
                Ports (Container→Host)
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-xl">
                Volumes (App→Host)
              </AppTableCell>
              <AppTableCell
                align="center"
                className="app-table-hide-below-xl"
                width="80px"
              >
                CPU / Mem
              </AppTableCell>
              <AppTableCell align="center" width="180px">
                Actions
              </AppTableCell>
            </AppTableRow>
          </AppTableHead>
          <AppTableBody>
            {containers.map((container, index) => (
              <ContainerRow
                container={container}
                editMode={editMode}
                index={index}
                key={container.Id}
              />
            ))}
          </AppTableBody>
        </AppTable>
      </AppTableContainer>
      {containers.length === 0 && (
        <div style={{ textAlign: "center", paddingBlock: 16 }}>
          <AppTypography color="text.secondary" variant="body2">
            No containers found.
          </AppTypography>
        </div>
      )}
    </div>
  );
};

export default ContainerTable;
