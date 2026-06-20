import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { Suspense, useCallback, useMemo, useState } from "react";

import ActionButton from "./ActionButton";

import { jobSnapshotResult, linuxio } from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getContainerStatusColor } from "@/constants/statusColors";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { TRANSITION_SLOW_CSS } from "@/theme/constants";
import type {
  ContainerEndpoint,
  ContainerInfo,
  ContainerMount,
  ContainerPort,
} from "@/types/container";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

const LogsDialog = React.lazy(() => import("@/pages/main/docker/LogsDialog"));
const TerminalDialog = React.lazy(
  () => import("@/pages/main/docker/TerminalDialog"),
);

const DOCKER_TOAST_META = { href: "/docker", label: "Open Docker" };

const getContainerName = (container: ContainerInfo) =>
  container.Names?.[0]?.replace("/", "") || "Unnamed";

const getDisplayState = (container: ContainerInfo) => {
  const s = container.Status.toLowerCase();
  if (s.includes("unhealthy")) return "Unhealthy";
  if (s.includes("healthy")) return "Healthy";
  if (container.State === "running") return "Running";
  if (container.State === "exited") return "Stopped";
  if (container.State === "dead") return "Dead";
  return container.State;
};

const getImageVersion = (image: string) => {
  const noDigest = image.split("@")[0];
  const parts = noDigest.split(":");
  if (parts.length < 2) return "-";
  return parts[parts.length - 1] || "-";
};

const getUpdateStatus = (container: ContainerInfo) => {
  if (container.updateError) {
    return {
      color: "error",
      label: "Error",
      title: container.updateError,
    };
  }
  if (container.updateAvailable === true) {
    return {
      color: "warning",
      label: "Update",
      title: "Update available",
    };
  }
  if (container.updateAvailable === false || container.updateCheckedAt) {
    return {
      color: "success",
      label: "Current",
      title: container.updateCheckedAt
        ? `Checked ${new Date(container.updateCheckedAt).toLocaleString()}`
        : "No update available",
    };
  }
  return {
    color: "default",
    label: "Unknown",
    title: "Not checked",
  };
};

const formatUptime = (createdUnix: number) => {
  const secs = Math.floor(Date.now() / 1000) - createdUnix;
  if (secs < 0) return "-";
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60) % 60;
  const h = Math.floor(secs / 3600) % 24;
  const d = Math.floor(secs / 86400);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
};

const getDedupedPorts = (container: ContainerInfo) => {
  const seen = new Set<string>();
  return (container.Ports ?? [])
    .filter((port) => {
      const key = port.PublicPort
        ? `${port.PrivatePort}/${port.Type}:${port.PublicPort}`
        : `${port.PrivatePort}/${port.Type}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .sort(
      (a, b) => a.PrivatePort - b.PrivatePort || a.Type.localeCompare(b.Type),
    );
};

const getMounts = (container: ContainerInfo) =>
  (container.Mounts ?? []).filter(
    (mount) => mount.Type === "bind" || mount.Type === "volume",
  );

function ContainerNameCell({ container }: { container: ContainerInfo }) {
  const name = getContainerName(container);
  const displayState = getDisplayState(container);

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
      <StatusDot
        color={getContainerStatusColor(displayState)}
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
  );
}

function VersionCell({ image }: { image: string }) {
  const version = getImageVersion(image);

  return (
    <AppTypography
      color="text.secondary"
      copyText={version}
      noWrap
      style={{
        fontFamily: "monospace",
        fontSize: "0.78rem",
      }}
      title={version}
      toastMeta={DOCKER_TOAST_META}
      variant="body2"
    >
      {version}
    </AppTypography>
  );
}

interface UpdateCellProps {
  checkingUpdates: boolean;
  container: ContainerInfo;
}

function UpdateCell({ checkingUpdates, container }: UpdateCellProps) {
  const name = getContainerName(container);
  const queryClient = useQueryClient();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const updateStatus = getUpdateStatus(container);
  const refreshContainerViews = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_containers.queryKey(),
    });
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_compose_projects.queryKey(),
    });
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_images.queryKey(),
    });
  }, [queryClient]);
  const { mutate: checkContainerUpdate, isPending: isCheckingUpdate } =
    linuxio.docker.check_container_update.useMutation({
      onSuccess: (data) => {
        const result =
          jobSnapshotResult<{
            checked?: number;
            updates?: number;
          }>(data) ?? {};
        const updates = result.updates ?? 0;
        toast.success(
          updates > 0
            ? `Container ${name} has an update`
            : `Container ${name} is up to date`,
        );
        refreshContainerViews();
      },
      onError: (err: Error) =>
        toast.error(
          getMutationErrorMessage(err, `Failed to check updates for ${name}`),
        ),
    });
  const { mutate: updateContainer, isPending: isUpdatePending } =
    linuxio.docker.update_container.useMutation({
      onSuccess: (data) => {
        const result = jobSnapshotResult<{ updated: boolean }>(data);
        toast.success(
          result.updated
            ? `Container ${name} updated`
            : `Container ${name} is already up to date`,
        );
        refreshContainerViews();
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to update ${name}`)),
    });

  return (
    <div
      style={{
        alignItems: "center",
        display: "flex",
        gap: 4,
        minWidth: 0,
      }}
    >
      <Chip
        color={updateStatus.color}
        disabled={isUpdatePending}
        label={
          isUpdatePending ? (
            <span
              style={{
                alignItems: "center",
                display: "inline-flex",
                gap: 4,
              }}
            >
              <AppCircularProgress color="inherit" size={12} />
              Updating
            </span>
          ) : (
            updateStatus.label
          )
        }
        onClick={
          container.updateAvailable
            ? () => updateContainer({ containerId: container.Id })
            : undefined
        }
        size="small"
        title={container.updateAvailable ? "Apply update" : updateStatus.title}
        variant="soft"
      />
      <AppTooltip title="Check for updates">
        <span>
          <ActionButton
            icon="mdi:magnify"
            loading={isCheckingUpdate || checkingUpdates}
            onClick={() => checkContainerUpdate({ containerId: container.Id })}
          />
        </span>
      </AppTooltip>
    </div>
  );
}

interface AutoUpdateCellProps {
  autoUpdateDisabled: boolean;
  autoUpdatePending: boolean;
  autoUpdateReason?: string;
  autoUpdateSelected: boolean;
  container: ContainerInfo;
  onToggleAutoUpdate: (name: string) => void;
}

function AutoUpdateCell({
  autoUpdateDisabled,
  autoUpdatePending,
  autoUpdateReason,
  autoUpdateSelected,
  container,
  onToggleAutoUpdate,
}: AutoUpdateCellProps) {
  const theme = useAppTheme();
  const [autoTooltipKey, setAutoTooltipKey] = useState(0);
  const name = getContainerName(container);
  const tooltip = autoUpdateDisabled
    ? autoUpdateReason
    : autoUpdatePending
      ? "Saving auto-update setting"
      : autoUpdateSelected
        ? "Scheduled auto-update enabled"
        : "Scheduled auto-update disabled";

  return (
    <AppTooltip key={autoTooltipKey} title={tooltip}>
      <span>
        <ActionButton
          color={autoUpdateSelected ? theme.palette.primary.main : undefined}
          disabled={autoUpdateDisabled || autoUpdatePending}
          icon="mdi:timer-cog-outline"
          loading={autoUpdatePending}
          onClick={() => {
            setAutoTooltipKey((key) => key + 1);
            onToggleAutoUpdate(name);
          }}
        />
      </span>
    </AppTooltip>
  );
}

function UptimeCell({ created }: { created: number }) {
  return (
    <AppTypography
      color="text.secondary"
      style={{
        fontFamily: "monospace",
        fontSize: "0.78rem",
        fontVariantNumeric: "tabular-nums",
      }}
      variant="body2"
    >
      {formatUptime(created)}
    </AppTypography>
  );
}

function NetworkCell({
  networks,
}: {
  networks: Array<[string, ContainerEndpoint]>;
}) {
  const theme = useAppTheme();
  const networkNamesText = networks
    .map(([networkName]) => networkName)
    .join(", ");

  if (networks.length === 0) {
    return (
      <AppTypography color="text.disabled" variant="body2">
        -
      </AppTypography>
    );
  }

  return (
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
  );
}

function NetworkAddressCell({
  networks,
}: {
  networks: Array<[string, ContainerEndpoint]>;
}) {
  const networkAddressesText = networks
    .map(
      ([networkName, endpoint]) =>
        `${networkName}: ${endpoint.IPAddress || "-"}`,
    )
    .join("\n");

  if (networks.length === 0 || !networks[0][1].IPAddress) {
    return (
      <AppTypography color="text.disabled" variant="body2">
        -
      </AppTypography>
    );
  }

  return (
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
  );
}

interface PortsCellProps {
  expanded: boolean;
  ports: ContainerPort[];
}

function PortsCell({ expanded, ports }: PortsCellProps) {
  const theme = useAppTheme();

  if (ports.length === 0) {
    return (
      <AppTypography color="text.disabled" variant="body2">
        -
      </AppTypography>
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
      {ports.slice(0, 2).map((port) => {
        const text = `${port.PrivatePort}/${port.Type} -> ${
          port.PublicPort ?? "-"
        }`;
        return (
          <AppTypography
            copyText={text}
            key={`${port.PrivatePort}-${port.PublicPort ?? "none"}-${port.Type}`}
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
              {port.PrivatePort}/{port.Type}
            </span>
            <span
              style={{
                color: theme.palette.text.disabled,
                marginInline: 2,
              }}
            >
              {"->"}
            </span>
            <span style={{ color: theme.palette.text.secondary }}>
              {port.PublicPort ?? "-"}
            </span>
          </AppTypography>
        );
      })}
      <AppCollapse in={expanded}>
        <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
          {ports.slice(2).map((port) => {
            const text = `${port.PrivatePort}/${port.Type} -> ${
              port.PublicPort ?? "-"
            }`;
            return (
              <AppTypography
                copyText={text}
                key={`${port.PrivatePort}-${port.PublicPort ?? "none"}-${port.Type}`}
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
                  {port.PrivatePort}/{port.Type}
                </span>
                <span
                  style={{
                    color: theme.palette.text.disabled,
                    marginInline: 2,
                  }}
                >
                  {"->"}
                </span>
                <span style={{ color: theme.palette.text.secondary }}>
                  {port.PublicPort ?? "-"}
                </span>
              </AppTypography>
            );
          })}
        </div>
      </AppCollapse>
      {ports.length > 2 && (
        <AppCollapse in={!expanded}>
          <AppTypography
            color="text.disabled"
            style={{
              opacity: expanded ? 0 : 1,
              transition: `opacity ${TRANSITION_SLOW_CSS}`,
            }}
            variant="caption"
          >
            +{ports.length - 2} more
          </AppTypography>
        </AppCollapse>
      )}
    </div>
  );
}

interface VolumesCellProps {
  expanded: boolean;
  mounts: ContainerMount[];
}

function VolumesCell({ expanded, mounts }: VolumesCellProps) {
  const theme = useAppTheme();

  if (mounts.length === 0) {
    return (
      <AppTypography color="text.disabled" variant="body2">
        -
      </AppTypography>
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
      {mounts.slice(0, 2).map((mount) => {
        const text = `${mount.Destination} -> ${mount.Source}`;
        return (
          <AppTypography
            copyText={text}
            key={`${mount.Destination}-${mount.Source}`}
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
              {mount.Destination}
            </span>
            <span
              style={{
                color: theme.palette.text.disabled,
                marginInline: 2,
              }}
            >
              {"->"}
            </span>
            <span style={{ color: theme.palette.text.secondary }}>
              {mount.Source}
            </span>
          </AppTypography>
        );
      })}
      <AppCollapse in={expanded}>
        <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
          {mounts.slice(2).map((mount) => {
            const text = `${mount.Destination} -> ${mount.Source}`;
            return (
              <AppTypography
                copyText={text}
                key={`${mount.Destination}-${mount.Source}`}
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
                  {mount.Destination}
                </span>
                <span
                  style={{
                    color: theme.palette.text.disabled,
                    marginInline: 2,
                  }}
                >
                  {"->"}
                </span>
                <span style={{ color: theme.palette.text.secondary }}>
                  {mount.Source}
                </span>
              </AppTypography>
            );
          })}
        </div>
      </AppCollapse>
      {mounts.length > 2 && (
        <AppCollapse in={!expanded}>
          <AppTypography
            color="text.disabled"
            style={{
              opacity: expanded ? 0 : 1,
              transition: `opacity ${TRANSITION_SLOW_CSS}`,
            }}
            variant="caption"
          >
            +{mounts.length - 2} more
          </AppTypography>
        </AppCollapse>
      )}
    </div>
  );
}

function MetricsCell({ container }: { container: ContainerInfo }) {
  const cpuPercent = container.metrics?.cpu_percent ?? 0;
  const memUsage = container.metrics?.mem_usage ?? 0;

  return (
    <div style={{ display: "flex", flexDirection: "column" }}>
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
    </div>
  );
}

interface ActionsCellProps {
  container: ContainerInfo;
  expanded: boolean;
  hasExpandableDetails: boolean;
  onToggleExpanded: () => void;
}

function ActionsCell({
  container,
  expanded,
  hasExpandableDetails,
  onToggleExpanded,
}: ActionsCellProps) {
  const name = getContainerName(container);
  const queryClient = useQueryClient();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const [logDialogOpen, setLogDialogOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [hasLoadedLogs, setHasLoadedLogs] = useState(false);
  const [hasLoadedTerminal, setHasLoadedTerminal] = useState(false);
  const refreshContainers = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_containers.queryKey(),
    });
  }, [queryClient]);
  const { mutate: startContainer } = linuxio.docker.start_container.useMutation(
    {
      onSuccess: () => {
        toast.success(`Container ${name} started`);
        refreshContainers();
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to start ${name}`)),
    },
  );
  const { mutate: stopContainer } = linuxio.docker.stop_container.useMutation({
    onSuccess: () => {
      toast.success(`Container ${name} stopped`);
      refreshContainers();
    },
    onError: (err: Error) =>
      toast.error(getMutationErrorMessage(err, `Failed to stop ${name}`)),
  });
  const { mutate: restartContainer } =
    linuxio.docker.restart_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} restarted`);
        refreshContainers();
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to restart ${name}`)),
    });
  const { mutate: removeContainer } =
    linuxio.docker.remove_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} removed`);
        refreshContainers();
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to remove ${name}`)),
    });

  return (
    <>
      <div
        style={{
          display: "flex",
          justifyContent: "flex-end",
          alignItems: "center",
          gap: 2,
        }}
      >
        {container.State !== "running" && (
          <AppTooltip title="Start">
            <span>
              <ActionButton
                icon="mdi:play"
                onClick={() => startContainer({ containerId: container.Id })}
              />
            </span>
          </AppTooltip>
        )}
        {container.State === "running" && (
          <AppTooltip title="Stop">
            <span>
              <ActionButton
                icon="mdi:stop"
                onClick={() => stopContainer({ containerId: container.Id })}
              />
            </span>
          </AppTooltip>
        )}
        <AppTooltip title="Restart">
          <span>
            <ActionButton
              icon="mdi:restart"
              onClick={() => restartContainer({ containerId: container.Id })}
            />
          </span>
        </AppTooltip>
        <AppTooltip title="Remove">
          <span>
            <ActionButton
              icon="mdi:delete"
              onClick={() => removeContainer({ containerId: container.Id })}
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
        {container.url && (
          <AppTooltip title="Open App">
            <span>
              <ActionButton
                icon="mdi:open-in-new"
                onClick={() => window.open(container.url, "_blank", "noopener")}
              />
            </span>
          </AppTooltip>
        )}
        <AppIconButton
          className="container-expand-toggle"
          onClick={onToggleExpanded}
          size="small"
          style={{
            marginLeft: 2,
            visibility: hasExpandableDetails ? "visible" : "hidden",
          }}
        >
          <Icon
            height={20}
            icon="mdi:chevron-down"
            style={{
              transform: expanded ? "rotate(180deg)" : "rotate(0deg)",
              transition: `transform ${TRANSITION_SLOW_CSS}`,
            }}
            width={20}
          />
        </AppIconButton>
      </div>

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
    </>
  );
}

interface ContainerTableProps {
  autoUpdateDisabled: boolean;
  autoUpdatePendingNames: Set<string>;
  autoUpdateReason?: string;
  autoUpdateSelectedNames: Set<string>;
  checkingUpdates?: boolean;
  containers: ContainerInfo[];
  editMode?: boolean;
  onToggleAutoUpdate: (name: string) => void;
}

const ContainerTable: React.FC<ContainerTableProps> = ({
  autoUpdateDisabled,
  autoUpdatePendingNames,
  autoUpdateReason,
  autoUpdateSelectedNames,
  checkingUpdates = false,
  containers,
  editMode = false,
  onToggleAutoUpdate,
}) => {
  const [expandedContainerIds, setExpandedContainerIds] = useState<Set<string>>(
    () => new Set(),
  );
  const toggleExpanded = useCallback((containerId: string) => {
    setExpandedContainerIds((previous) => {
      const next = new Set(previous);
      if (next.has(containerId)) {
        next.delete(containerId);
      } else {
        next.add(containerId);
      }
      return next;
    });
  }, []);
  const columns = useMemo<AppDataTableColumnDef<ContainerInfo>[]>(
    () => [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => <ContainerNameCell container={row.original} />,
        meta: { align: "left" },
      },
      {
        id: "version",
        header: "Version",
        cell: ({ row }) => <VersionCell image={row.original.Image} />,
        meta: {
          hideBelow: "md",
          width: "160px",
        },
      },
      {
        id: "update",
        header: "Update",
        cell: ({ row }) => (
          <UpdateCell
            checkingUpdates={checkingUpdates}
            container={row.original}
          />
        ),
        meta: {
          hideBelow: "md",
          width: "140px",
        },
      },
      {
        id: "auto",
        header: "Auto",
        cell: ({ row }) => {
          const name = getContainerName(row.original);
          return (
            <AutoUpdateCell
              autoUpdateDisabled={autoUpdateDisabled}
              autoUpdatePending={autoUpdatePendingNames.has(name)}
              autoUpdateReason={autoUpdateReason}
              autoUpdateSelected={autoUpdateSelectedNames.has(name)}
              container={row.original}
              onToggleAutoUpdate={onToggleAutoUpdate}
            />
          );
        },
        meta: {
          align: "center",
          width: "60px",
        },
      },
      {
        id: "uptime",
        header: "Uptime",
        cell: ({ row }) => <UptimeCell created={row.original.Created} />,
        meta: { hideBelow: "md" },
      },
      {
        id: "network",
        header: "Network",
        cell: ({ row }) => (
          <NetworkCell
            networks={Object.entries(
              row.original.NetworkSettings?.Networks ?? {},
            )}
          />
        ),
        meta: { hideBelow: "lg" },
      },
      {
        id: "ip",
        header: "Container IP",
        cell: ({ row }) => (
          <NetworkAddressCell
            networks={Object.entries(
              row.original.NetworkSettings?.Networks ?? {},
            )}
          />
        ),
        meta: { hideBelow: "lg" },
      },
      {
        id: "ports",
        header: "Ports (Container->Host)",
        cell: ({ row }) => (
          <PortsCell
            expanded={expandedContainerIds.has(row.original.Id)}
            ports={getDedupedPorts(row.original)}
          />
        ),
        meta: {
          hideBelow: "xl",
          width: "160px",
        },
      },
      {
        id: "volumes",
        header: "Volumes (App->Host)",
        cell: ({ row }) => (
          <VolumesCell
            expanded={expandedContainerIds.has(row.original.Id)}
            mounts={getMounts(row.original)}
          />
        ),
        meta: {
          hideBelow: "xl",
          cellStyle: { maxWidth: 280 },
        },
      },
      {
        id: "metrics",
        header: "CPU / Mem",
        cell: ({ row }) => <MetricsCell container={row.original} />,
        meta: {
          align: "center",
          hideBelow: "xl",
          width: "80px",
        },
      },
      {
        id: "actions",
        header: "Actions",
        enableSorting: false,
        cell: ({ row }) => {
          const ports = getDedupedPorts(row.original);
          const mounts = getMounts(row.original);
          const expanded = expandedContainerIds.has(row.original.Id);
          return (
            <ActionsCell
              container={row.original}
              expanded={expanded}
              hasExpandableDetails={ports.length > 2 || mounts.length > 2}
              onToggleExpanded={() => toggleExpanded(row.original.Id)}
            />
          );
        },
        meta: {
          align: "right",
          width: "180px",
        },
      },
    ],
    [
      autoUpdateDisabled,
      autoUpdatePendingNames,
      autoUpdateReason,
      autoUpdateSelectedNames,
      checkingUpdates,
      expandedContainerIds,
      onToggleAutoUpdate,
      toggleExpanded,
    ],
  );

  return (
    <AppDataTable
      ariaLabel="Docker containers"
      columns={columns}
      data={containers}
      dnd={
        editMode
          ? {
              getItemId: (row) => row.original.Id,
              handleAriaLabel: "Reorder container",
              handleColumnWidth: 28,
            }
          : undefined
      }
      emptyMessage="No containers found."
      enableSorting={false}
      getRowId={(container) => container.Id}
    />
  );
};

export default ContainerTable;
