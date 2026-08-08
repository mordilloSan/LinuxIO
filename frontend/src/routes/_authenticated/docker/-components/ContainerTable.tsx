import { Icon } from "@iconify/react";
import {
  createContext,
  lazy,
  memo,
  Suspense,
  useCallback,
  useContext,
  useMemo,
  useState,
  type MouseEvent,
} from "react";

import {
  linuxio,
  type ContainerEndpoint,
  type ContainerInfo,
  type ContainerMount,
  type ContainerPort,
} from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import AppDataTable from "@/components/tables/AppDataTable";
import type {
  AppDataTableColumnDef,
  AppDataTableDndOptions,
} from "@/components/tables/AppDataTable";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getContainerStatusColor } from "@/constants/statusColors";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

import "./container-table.css";

const LogsDialog = lazy(() => import("@/components/docker/LogsDialog"));
const TerminalDialog = lazy(() => import("@/components/docker/TerminalDialog"));

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;

// Expanded-row state is shared with the cells via context rather than baked into
// the column definitions. flexRender renders a cell function as a component, so
// rebuilding the columns on every toggle would change each cell's component
// identity and remount its subtree — which restarts AppCollapse already-open and
// skips the expand animation. Context lets the columns stay stable while cells
// still re-render in place when the expanded set changes.
const ExpandedContainersContext = createContext<ReadonlySet<string>>(new Set());

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

// Docker merges the image's labels into the container's, so a well-behaved
// image tells us what "latest" currently resolves to.
const VERSION_LABEL_KEYS = [
  "org.opencontainers.image.version",
  "org.label-schema.version",
  "version",
];

const getLabelVersion = (labels?: Record<string, string>) => {
  if (!labels) return undefined;
  for (const key of VERSION_LABEL_KEYS) {
    const value = labels[key]?.trim();
    if (value) return value;
  }
  return undefined;
};

// A floating tag says nothing about what is actually running, so pair it with
// the labelled version when the image publishes one.
const getVersionDisplay = (container: ContainerInfo) => {
  const tag = getImageVersion(container.Image);
  const labelVersion = getLabelVersion(container.Labels);

  if (!labelVersion || labelVersion === tag) return tag;
  if (tag === "-") return labelVersion;
  if (tag === "latest") return `${tag} (${labelVersion})`;
  return tag;
};

type ContainerUpdateStatusInput = Pick<
  ContainerInfo,
  "updateAvailable" | "updateCheckedAt" | "updateError"
>;

const getUpdateStatus = ({
  updateAvailable,
  updateCheckedAt,
  updateError,
}: ContainerUpdateStatusInput) => {
  if (updateError) {
    return {
      dotColor: "var(--app-palette-error-main)",
      title: updateError,
    };
  }
  if (updateAvailable === true) {
    return {
      dotColor: "var(--app-palette-warning-main)",
      title: "Update available",
    };
  }
  if (updateAvailable === false || updateCheckedAt) {
    return {
      dotColor: "var(--app-palette-success-main)",
      title: updateCheckedAt
        ? `Up to date — checked ${new Date(updateCheckedAt).toLocaleString()}`
        : "Up to date",
    };
  }
  return {
    dotColor: "var(--app-palette-text-disabled)",
    title: "Not checked yet",
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

// A collapsed ports/volumes cell renders two entries plus a "+N more" caption,
// which is exactly the height an untruncated three-entry list takes. Collapsing
// only buys space from the fourth entry on, so three entries stay fully visible
// and the row offers no expander.
const COLLAPSED_ENTRIES = 2;

const isCollapsible = (total: number) => total > COLLAPSED_ENTRIES + 1;

const getVisibleEntries = (total: number) =>
  isCollapsible(total) ? COLLAPSED_ENTRIES : total;

const getMounts = (container: ContainerInfo) =>
  (container.Mounts ?? []).filter(
    (mount) => mount.Type === "bind" || mount.Type === "volume",
  );

const getContainerTableSignature = (container: ContainerInfo) => {
  const networks = Object.entries(container.NetworkSettings?.Networks ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(
      ([networkName, endpoint]) =>
        `${networkName}:${endpoint.IPAddress || "-"}`,
    )
    .join("|");
  const ports = getDedupedPorts(container)
    .map((port) => `${port.PrivatePort}:${port.PublicPort ?? "-"}:${port.Type}`)
    .join("|");
  const mounts = getMounts(container)
    .map((mount) => `${mount.Type}:${mount.Destination}:${mount.Source}`)
    .join("|");

  return [
    container.Id,
    container.Names?.join("|") ?? "",
    container.Image,
    container.State,
    container.Status,
    container.Created,
    container.icon ?? "",
    container.url ?? "",
    container.updateAvailable === undefined
      ? ""
      : String(container.updateAvailable),
    container.updateCheckedAt ?? "",
    container.updateError ?? "",
    container.metrics?.cpu_percent?.toFixed(1) ?? "",
    container.metrics?.mem_usage === undefined
      ? ""
      : formatFileSize(container.metrics.mem_usage),
    networks,
    ports,
    mounts,
  ].join("\u001f");
};

const EMPTY_STOPPING_CONTAINER_IDS = new Set<string>();

// AppTooltip marks its click-to-copy triggers with this class; a click on one
// copies a value and must not double as a row selection.
const INTERACTIVE_CELL_SELECTOR = ".app-tooltip-trigger--copy";

const areStringSetsEqual = (
  left: ReadonlySet<string>,
  right: ReadonlySet<string>,
) => {
  if (left === right) return true;
  if (left.size !== right.size) return false;
  for (const value of left) {
    if (!right.has(value)) return false;
  }
  return true;
};

const areContainerArraysEquivalent = (
  left: ContainerInfo[],
  right: ContainerInfo[],
) => {
  if (left === right) return true;
  if (left.length !== right.length) return false;
  for (let index = 0; index < left.length; index += 1) {
    if (
      getContainerTableSignature(left[index]) !==
      getContainerTableSignature(right[index])
    ) {
      return false;
    }
  }
  return true;
};

const asContainer = (row: unknown) => row as ContainerInfo;

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

function VersionCell({ version }: { version: string }) {
  return (
    <AppTypography
      className="container-table__version-text"
      color="text.secondary"
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
  containerId: string;
  name: string;
  updateAvailable?: boolean;
  updateCheckedAt?: ContainerInfo["updateCheckedAt"];
  updateError?: string;
}

const UpdateCell = memo(function UpdateCell({
  checkingUpdates,
  containerId,
  name,
  updateAvailable,
  updateCheckedAt,
  updateError,
}: UpdateCellProps) {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const updateStatus = getUpdateStatus({
    updateAvailable,
    updateCheckedAt,
    updateError,
  });
  const { mutate: checkContainerUpdate, isPending: isCheckingUpdate } =
    linuxio.docker.check_container_update.useAction({
      success: (result) => {
        const updates = result?.updates ?? 0;
        toast.success(
          updates > 0
            ? `Container ${name} has an update`
            : `Container ${name} is up to date`,
        );
      },
      error: `Failed to check updates for ${name}`,
      toast: DOCKER_TOAST_META,
    });
  const { mutate: updateContainer, isPending: isUpdatePending } =
    linuxio.docker.update_container.useAction({
      success: (result) => {
        toast.success(
          result.updated
            ? `Container ${name} updated`
            : `Container ${name} is already up to date`,
        );
      },
      error: `Failed to update ${name}`,
      toast: DOCKER_TOAST_META,
    });

  const checking = isCheckingUpdate || checkingUpdates;

  // An actionable row gets the chip instead of a dot: it is both the amber
  // state and the way to apply the update, and re-checking a container that
  // already has one pending buys nothing.
  if (updateAvailable === true) {
    return (
      <Chip
        color="warning"
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
            "Update"
          )
        }
        onClick={() => updateContainer({ containerId })}
        size="small"
        title="Apply update"
        variant="soft"
      />
    );
  }

  return (
    <AppTooltip
      title={
        checking
          ? "Checking for updates"
          : `${updateStatus.title} — click to check for updates`
      }
    >
      <button
        aria-label={`Check ${name} for updates`}
        className="container-table__update-dot"
        disabled={checking}
        onClick={() => checkContainerUpdate({ containerId })}
        type="button"
      >
        {checking ? (
          <AppCircularProgress color="inherit" size={10} />
        ) : (
          <span
            className="container-table__update-dot-mark"
            style={{ backgroundColor: updateStatus.dotColor }}
          />
        )}
      </button>
    </AppTooltip>
  );
});

interface AutoUpdateCellProps {
  autoUpdateDisabled: boolean;
  autoUpdatePending: boolean;
  autoUpdateReason?: string;
  autoUpdateSelected: boolean;
  name: string;
  onToggleAutoUpdate: (name: string) => void;
}

const AutoUpdateCell = memo(function AutoUpdateCell({
  autoUpdateDisabled,
  autoUpdatePending,
  autoUpdateReason,
  autoUpdateSelected,
  name,
  onToggleAutoUpdate,
}: AutoUpdateCellProps) {
  const theme = useAppTheme();
  const [autoTooltipKey, setAutoTooltipKey] = useState(0);
  const tooltip = autoUpdateDisabled
    ? (autoUpdateReason ?? "Scheduled auto-update unavailable")
    : autoUpdatePending
      ? "Saving auto-update setting"
      : autoUpdateSelected
        ? "Scheduled auto-update enabled"
        : "Scheduled auto-update disabled";

  return (
    <AppTooltip key={autoTooltipKey} title={tooltip}>
      <span>
        <AppActionIconButton
          color={autoUpdateSelected ? theme.palette.primary.main : undefined}
          disabled={autoUpdateDisabled || autoUpdatePending}
          icon="mdi:timer-cog-outline"
          iconSize={16}
          label={tooltip}
          loading={autoUpdatePending}
          onClick={() => {
            setAutoTooltipKey((key) => key + 1);
            onToggleAutoUpdate(name);
          }}
          tooltip={false}
        />
      </span>
    </AppTooltip>
  );
});

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

interface StackToggleProps {
  containerId: string;
  expanded: boolean;
  hiddenCount: number;
  onToggleExpanded: (containerId: string) => void;
}

// The "+N more" line is the row's expander: it only exists on rows that have
// something to reveal, and it sits next to the entries it is counting.
function StackToggle({
  containerId,
  expanded,
  hiddenCount,
  onToggleExpanded,
}: StackToggleProps) {
  return (
    <button
      aria-expanded={expanded}
      className="container-table__stack-toggle"
      onClick={() => onToggleExpanded(containerId)}
      type="button"
    >
      {expanded ? "Show less" : `+${hiddenCount} more`}
      <Icon
        className="container-table__stack-chevron"
        height={14}
        icon="mdi:chevron-down"
        width={14}
      />
    </button>
  );
}

interface PortsCellProps {
  containerId: string;
  onToggleExpanded: (containerId: string) => void;
  ports: ContainerPort[];
}

function PortsCell({ containerId, onToggleExpanded, ports }: PortsCellProps) {
  const theme = useAppTheme();
  const expanded = useContext(ExpandedContainersContext).has(containerId);

  if (ports.length === 0) {
    return (
      <div className="container-table__stack">
        <AppTypography color="text.disabled" variant="body2">
          -
        </AppTypography>
      </div>
    );
  }

  const visible = getVisibleEntries(ports.length);

  return (
    <div className="container-table__stack">
      {ports.slice(0, visible).map((port) => {
        const text = `${port.PrivatePort}/${port.Type} -> ${
          port.PublicPort ?? "-"
        }`;
        return (
          <AppTypography
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
        <div className="container-table__stack-group">
          {ports.slice(visible).map((port) => {
            const text = `${port.PrivatePort}/${port.Type} -> ${
              port.PublicPort ?? "-"
            }`;
            return (
              <AppTypography
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
      {isCollapsible(ports.length) && (
        <StackToggle
          containerId={containerId}
          expanded={expanded}
          hiddenCount={ports.length - visible}
          onToggleExpanded={onToggleExpanded}
        />
      )}
    </div>
  );
}

interface VolumesCellProps {
  containerId: string;
  mounts: ContainerMount[];
  onToggleExpanded: (containerId: string) => void;
}

function VolumesCell({
  containerId,
  mounts,
  onToggleExpanded,
}: VolumesCellProps) {
  const theme = useAppTheme();
  const expanded = useContext(ExpandedContainersContext).has(containerId);

  if (mounts.length === 0) {
    return (
      <div className="container-table__stack">
        <AppTypography color="text.disabled" variant="body2">
          -
        </AppTypography>
      </div>
    );
  }

  const visible = getVisibleEntries(mounts.length);

  return (
    <div className="container-table__stack">
      {mounts.slice(0, visible).map((mount) => {
        const text = `${mount.Destination} -> ${mount.Source}`;
        return (
          <AppTypography
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
        <div className="container-table__stack-group">
          {mounts.slice(visible).map((mount) => {
            const text = `${mount.Destination} -> ${mount.Source}`;
            return (
              <AppTypography
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
      {isCollapsible(mounts.length) && (
        <StackToggle
          containerId={containerId}
          expanded={expanded}
          hiddenCount={mounts.length - visible}
          onToggleExpanded={onToggleExpanded}
        />
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
        noWrap
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
        noWrap
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

interface ContainerAction {
  icon: string;
  label: string;
  loading?: boolean;
  onClick: () => void;
}

interface ActionsCellProps {
  autoUpdateDisabled: boolean;
  autoUpdatePending: boolean;
  autoUpdateReason?: string;
  autoUpdateSelected: boolean;
  // Below md only Name and Actions remain, and a seven-icon strip leaves the
  // name barely legible — so the same actions collapse into one menu, which
  // also swallows the auto-update toggle that sits beside the strip.
  compact: boolean;
  containerId: string;
  name: string;
  onOpenLogs: (containerId: string, containerName: string) => void;
  onOpenTerminal: (containerId: string, containerName: string) => void;
  onToggleAutoUpdate: (name: string) => void;
  pending?: boolean;
  state: string;
  url?: string;
}

const ActionsCell = memo(function ActionsCell({
  autoUpdateDisabled,
  autoUpdatePending,
  autoUpdateReason,
  autoUpdateSelected,
  compact,
  containerId,
  name,
  onOpenLogs,
  onOpenTerminal,
  onToggleAutoUpdate,
  pending = false,
  state,
  url,
}: ActionsCellProps) {
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const { mutate: startContainer } = linuxio.docker.start_container.useAction({
    success: `Container ${name} started`,
    error: `Failed to start ${name}`,
    toast: DOCKER_TOAST_META,
  });
  const { mutate: stopContainer } = linuxio.docker.stop_container.useAction({
    success: `Container ${name} stopped`,
    error: `Failed to stop ${name}`,
    toast: DOCKER_TOAST_META,
  });
  const { mutate: restartContainer } =
    linuxio.docker.restart_container.useAction({
      success: `Container ${name} restarted`,
      error: `Failed to restart ${name}`,
      toast: DOCKER_TOAST_META,
    });
  const { mutate: removeContainer } = linuxio.docker.remove_container.useAction(
    {
      success: `Container ${name} removed`,
      error: `Failed to remove ${name}`,
      toast: DOCKER_TOAST_META,
    },
  );

  const actions: ContainerAction[] = [
    state === "running"
      ? {
          icon: "mdi:stop",
          label: "Stop",
          loading: pending,
          onClick: () => stopContainer({ containerId }),
        }
      : {
          icon: "mdi:play",
          label: "Start",
          onClick: () => startContainer({ containerId }),
        },
    {
      icon: "mdi:restart",
      label: "Restart",
      onClick: () => restartContainer({ containerId }),
    },
    {
      icon: "mdi:delete",
      label: "Remove",
      onClick: () => removeContainer({ containerId }),
    },
    {
      icon: "mdi:file-document-outline",
      label: "Logs",
      onClick: () => onOpenLogs(containerId, name),
    },
    {
      icon: "mdi:console",
      label: "Terminal",
      onClick: () => onOpenTerminal(containerId, name),
    },
    ...(url
      ? [
          {
            icon: "mdi:open-in-new",
            label: "Open App",
            onClick: () => window.open(url, "_blank", "noopener"),
          },
        ]
      : []),
  ];

  if (compact) {
    return (
      <>
        <AppActionIconButton
          ariaLabel={`Actions for ${name}`}
          icon="mdi:dots-vertical"
          iconSize={20}
          onClick={(event) => setMenuAnchor(event.currentTarget)}
          tooltip={false}
        />
        <AppMenu
          anchorEl={menuAnchor}
          minWidth={160}
          onClose={() => setMenuAnchor(null)}
          open={Boolean(menuAnchor)}
        >
          {actions.map((action) => (
            <AppMenuItem
              disabled={pending}
              key={action.label}
              onClick={() => {
                setMenuAnchor(null);
                action.onClick();
              }}
              startAdornment={<Icon icon={action.icon} width={18} />}
            >
              {action.label}
            </AppMenuItem>
          ))}
          <AppDivider />
          <AppMenuItem
            disabled={autoUpdateDisabled || autoUpdatePending}
            endAdornment={
              autoUpdateSelected ? <Icon icon="mdi:check" width={16} /> : null
            }
            onClick={() => {
              setMenuAnchor(null);
              onToggleAutoUpdate(name);
            }}
            selected={autoUpdateSelected}
            startAdornment={<Icon icon="mdi:timer-cog-outline" width={18} />}
            title={
              autoUpdateDisabled
                ? (autoUpdateReason ?? "Scheduled auto-update unavailable")
                : undefined
            }
          >
            Auto-update
          </AppMenuItem>
        </AppMenu>
      </>
    );
  }

  return (
    <div
      style={{
        display: "flex",
        justifyContent: "flex-end",
        alignItems: "center",
        gap: 2,
      }}
    >
      {actions.map((action) => (
        // The outer tooltip wraps a span because a disabled button emits no
        // hover events of its own.
        <AppTooltip key={action.label} title={action.label}>
          <span>
            <AppActionIconButton
              disabled={pending && !action.loading}
              icon={action.icon}
              iconSize={16}
              label={action.label}
              loading={action.loading}
              onClick={action.onClick}
              tooltip={false}
            />
          </span>
        </AppTooltip>
      ))}
    </div>
  );
});

interface ContainerTableProps {
  autoUpdateDisabled: boolean;
  autoUpdatePendingNames: Set<string>;
  autoUpdateReason?: string;
  autoUpdateSelectedNames: Set<string>;
  checkingUpdates?: boolean;
  containers: ContainerInfo[];
  editMode?: boolean;
  onSelectContainer?: (containerId: string) => void;
  onToggleAutoUpdate: (name: string) => void;
  stoppingContainerIds?: ReadonlySet<string>;
}

interface ContainerDialogTarget {
  id: string;
  name: string;
}

const ContainerTable = ({
  autoUpdateDisabled,
  autoUpdatePendingNames,
  autoUpdateReason,
  autoUpdateSelectedNames,
  checkingUpdates = false,
  containers,
  editMode = false,
  onSelectContainer,
  onToggleAutoUpdate,
  stoppingContainerIds = EMPTY_STOPPING_CONTAINER_IDS,
}: ContainerTableProps) => {
  const theme = useAppTheme();
  // Same breakpoint the Version and Uptime columns hide at, so the strip
  // collapses exactly when Actions starts crowding the name.
  const compactActions = useAppMediaQuery(theme.breakpoints.down("md"));
  const [expandedContainerIds, setExpandedContainerIds] = useState<Set<string>>(
    () => new Set(),
  );
  const [logsTarget, setLogsTarget] = useState<ContainerDialogTarget | null>(
    null,
  );
  const [terminalTarget, setTerminalTarget] =
    useState<ContainerDialogTarget | null>(null);
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
  const openLogs = useCallback((containerId: string, containerName: string) => {
    setLogsTarget({ id: containerId, name: containerName });
  }, []);
  const openTerminal = useCallback(
    (containerId: string, containerName: string) => {
      setTerminalTarget({ id: containerId, name: containerName });
    },
    [],
  );
  const handleRowClick = useCallback(
    (row: { original: ContainerInfo }, event: MouseEvent) => {
      // A row is a container of controls before it is a link: the action
      // buttons, the "+N more" expander, the update dot and the copy-on-click
      // cells all bubble up to here and must not navigate.
      if (
        (event.target as HTMLElement).closest(
          `button, a, input, ${INTERACTIVE_CELL_SELECTOR}`,
        )
      ) {
        return;
      }
      onSelectContainer?.(row.original.Id);
    },
    [onSelectContainer],
  );
  const columns = useMemo<AppDataTableColumnDef<ContainerInfo>[]>(
    () => [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => <ContainerNameCell container={row.original} />,
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const container = asContainer(row);
            return [
              container.Id,
              getContainerName(container),
              getDisplayState(container),
              container.icon,
            ];
          },
          width: "minmax(0, 1.6fr)",
        },
      },
      {
        id: "version",
        header: "Version",
        cell: ({ row }) => {
          const container = row.original;
          return (
            <div className="container-table__version-row">
              <VersionCell version={getVersionDisplay(container)} />
              <UpdateCell
                checkingUpdates={checkingUpdates}
                containerId={container.Id}
                name={getContainerName(container)}
                updateAvailable={container.updateAvailable}
                updateCheckedAt={container.updateCheckedAt}
                updateError={container.updateError}
              />
            </div>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const container = asContainer(row);
            return [
              container.Id,
              getVersionDisplay(container),
              getContainerName(container),
              container.updateAvailable,
              container.updateCheckedAt,
              container.updateError,
              checkingUpdates,
            ];
          },
          hideBelow: "md",
          width: "170px",
        },
      },
      {
        id: "uptime",
        header: "Uptime",
        cell: ({ row }) => <UptimeCell created={row.original.Created} />,
        meta: { hideBelow: "md", width: "90px" },
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
        meta: {
          getCellRenderKey: (row) => {
            const container = asContainer(row);
            return [
              container.Id,
              Object.keys(container.NetworkSettings?.Networks ?? {})
                .sort()
                .join("|"),
            ];
          },
          hideBelow: "lg",
        },
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
        meta: {
          getCellRenderKey: (row) => {
            const container = asContainer(row);
            return [
              container.Id,
              Object.entries(container.NetworkSettings?.Networks ?? {})
                .sort(([left], [right]) => left.localeCompare(right))
                .map(
                  ([networkName, endpoint]) =>
                    `${networkName}:${endpoint.IPAddress || "-"}`,
                )
                .join("|"),
            ];
          },
          hideBelow: "lg",
          width: "130px",
        },
      },
      {
        id: "ports",
        header: () => (
          <AppTooltip placement="top" title="Container -> Host">
            <span className="container-table__header-label">Ports</span>
          </AppTooltip>
        ),
        cell: ({ row }) => (
          <PortsCell
            containerId={row.original.Id}
            onToggleExpanded={toggleExpanded}
            ports={getDedupedPorts(row.original)}
          />
        ),
        meta: {
          hideBelow: "xl",
          width: "minmax(130px, 155px)",
          cellStyle: { alignItems: "flex-start" },
          getCellRenderKey: (row) => {
            const container = asContainer(row);
            return [
              container.Id,
              getDedupedPorts(container)
                .map(
                  (port) =>
                    `${port.PrivatePort}:${port.PublicPort ?? "-"}:${port.Type}`,
                )
                .join("|"),
            ];
          },
        },
      },
      {
        id: "volumes",
        header: () => (
          <AppTooltip placement="top" title="App -> Host">
            <span className="container-table__header-label">Volumes</span>
          </AppTooltip>
        ),
        cell: ({ row }) => (
          <VolumesCell
            containerId={row.original.Id}
            mounts={getMounts(row.original)}
            onToggleExpanded={toggleExpanded}
          />
        ),
        meta: {
          hideBelow: "xl",
          width: "minmax(0, 2.2fr)",
          cellStyle: { alignItems: "flex-start" },
          getCellRenderKey: (row) => {
            const container = asContainer(row);
            return [
              container.Id,
              getMounts(container)
                .map(
                  (mount) =>
                    `${mount.Type}:${mount.Destination}:${mount.Source}`,
                )
                .join("|"),
            ];
          },
        },
      },
      {
        id: "metrics",
        header: "CPU / Mem",
        cell: ({ row }) => <MetricsCell container={row.original} />,
        meta: {
          align: "center",
          getCellRenderKey: (row) => {
            const container = asContainer(row);
            return [
              container.Id,
              (container.metrics?.cpu_percent ?? 0).toFixed(1),
              formatFileSize(container.metrics?.mem_usage ?? 0),
            ];
          },
          hideBelow: "xl",
          width: "110px",
        },
      },
      {
        id: "actions",
        header: "Actions",
        enableSorting: false,
        cell: ({ row }) => {
          const container = row.original;
          const name = getContainerName(container);
          return (
            <>
              {!compactActions && (
                <AutoUpdateCell
                  autoUpdateDisabled={autoUpdateDisabled}
                  autoUpdatePending={autoUpdatePendingNames.has(name)}
                  autoUpdateReason={autoUpdateReason}
                  autoUpdateSelected={autoUpdateSelectedNames.has(name)}
                  name={name}
                  onToggleAutoUpdate={onToggleAutoUpdate}
                />
              )}
              <ActionsCell
                autoUpdateDisabled={autoUpdateDisabled}
                autoUpdatePending={autoUpdatePendingNames.has(name)}
                autoUpdateReason={autoUpdateReason}
                autoUpdateSelected={autoUpdateSelectedNames.has(name)}
                compact={compactActions}
                containerId={container.Id}
                name={name}
                onOpenLogs={openLogs}
                onOpenTerminal={openTerminal}
                onToggleAutoUpdate={onToggleAutoUpdate}
                pending={stoppingContainerIds.has(container.Id)}
                state={container.State}
                url={container.url}
              />
            </>
          );
        },
        meta: {
          align: "right",
          // The buttons fill the column, so a right-aligned label reads as
          // hanging off the end of the strip rather than titling it.
          headerStyle: { justifyContent: "center" },
          // Tighter than the default 16px so the seven-button strip fits the
          // track without clipping, and gap: 2 matches the strip's own spacing
          // across the auto-update toggle sitting beside it.
          cellStyle: { gap: 2, paddingInline: 8 },
          // A compact row holds nothing but the menu button, so the rest of the
          // track goes back to the name.
          width: compactActions ? "56px" : "215px",
          getCellRenderKey: (row) => {
            const container = asContainer(row);
            const name = getContainerName(container);
            return [
              container.Id,
              name,
              container.State,
              container.url,
              stoppingContainerIds.has(container.Id),
              autoUpdateDisabled,
              autoUpdatePendingNames.has(name),
              autoUpdateReason,
              autoUpdateSelectedNames.has(name),
            ];
          },
        },
      },
    ],
    [
      autoUpdateDisabled,
      autoUpdatePendingNames,
      autoUpdateReason,
      autoUpdateSelectedNames,
      checkingUpdates,
      compactActions,
      openLogs,
      openTerminal,
      onToggleAutoUpdate,
      stoppingContainerIds,
      toggleExpanded,
    ],
  );
  const dnd = useMemo<AppDataTableDndOptions<ContainerInfo> | undefined>(
    () =>
      editMode
        ? {
            getItemId: (row) => row.original.Id,
            handleAriaLabel: "Reorder container",
            handleColumnWidth: 28,
          }
        : undefined,
    [editMode],
  );

  return (
    <>
      <ExpandedContainersContext.Provider value={expandedContainerIds}>
        <AppDataTable
          ariaLabel="Docker containers"
          columns={columns}
          data={containers}
          dnd={dnd}
          emptyMessage="No containers found."
          enableSorting={false}
          getRowId={(container) => container.Id}
          // Dragging rows is the point of edit mode; selecting one there would
          // fight the drag and immediately swap the table for a detail view.
          onRowClick={
            onSelectContainer && !editMode ? handleRowClick : undefined
          }
        />
      </ExpandedContainersContext.Provider>
      <Suspense fallback={null}>
        {logsTarget && (
          <LogsDialog
            containerId={logsTarget.id}
            containerName={logsTarget.name}
            onClose={() => setLogsTarget(null)}
            open
          />
        )}
        {terminalTarget && (
          <TerminalDialog
            containerId={terminalTarget.id}
            containerName={terminalTarget.name}
            onClose={() => setTerminalTarget(null)}
            open
          />
        )}
      </Suspense>
    </>
  );
};

const areContainerTablePropsEqual = (
  previous: ContainerTableProps,
  next: ContainerTableProps,
) =>
  previous.autoUpdateDisabled === next.autoUpdateDisabled &&
  previous.autoUpdateReason === next.autoUpdateReason &&
  previous.checkingUpdates === next.checkingUpdates &&
  previous.editMode === next.editMode &&
  previous.onSelectContainer === next.onSelectContainer &&
  previous.onToggleAutoUpdate === next.onToggleAutoUpdate &&
  areStringSetsEqual(
    previous.stoppingContainerIds ?? EMPTY_STOPPING_CONTAINER_IDS,
    next.stoppingContainerIds ?? EMPTY_STOPPING_CONTAINER_IDS,
  ) &&
  areStringSetsEqual(
    previous.autoUpdatePendingNames,
    next.autoUpdatePendingNames,
  ) &&
  areStringSetsEqual(
    previous.autoUpdateSelectedNames,
    next.autoUpdateSelectedNames,
  ) &&
  areContainerArraysEquivalent(previous.containers, next.containers);

export default memo(ContainerTable, areContainerTablePropsEqual);
