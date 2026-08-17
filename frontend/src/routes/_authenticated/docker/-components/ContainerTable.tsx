import { Icon } from "@iconify/react";
import type { Row } from "@tanstack/react-table";
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
  useCallMutation,
} from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import { useDockerUpdateOperation } from "@/components/docker/DockerUpdateOperationProvider";
import AppDataTable from "@/components/tables/AppDataTable";
import type {
  AppDataTableColumnDef,
  AppDataTableDndOptions,
  AppDataTableRowAttributes,
  AppDataTableRowRenderProps,
} from "@/components/tables/AppDataTable";
import type {
  AppDataTableCellRenderKey,
  AppTableFeatures,
} from "@/components/tables/AppDataTable.types";
import { clickTargetsRowBody } from "@/components/tables/rowInteraction";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getContainerStatusColor } from "@/constants/statusColors";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { formatFileSize, formatRelativeAge } from "@/utils/formaters";

import {
  buildContainerTableRows,
  formatStackSummary,
  getComposeProject,
  isStackHeaderRow,
  summarizeStack,
  type ContainerStackHeaderRow,
  type ContainerTableRow,
} from "./containerStacks";

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

// And so does the page-wide "checking updates" flag, which toggles twice per
// check — once on click, once when the sweep returns, the second one landing
// long after the pointer has moved back over the rows.
const CheckingUpdatesContext = createContext(false);

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
  | "updateAvailable"
  | "updateCheckedAt"
  | "updateCheckReason"
  | "updateCheckState"
  | "updateError"
>;

// `label` is the visible second line of the Version cell; `detail` is the
// tooltip's first line — how old the verdict is, which the label deliberately
// does not claim, since a scan is only as current as when it last ran.
const getUpdateStatus = ({
  updateAvailable,
  updateCheckedAt,
  updateCheckReason,
  updateCheckState,
  updateError,
}: ContainerUpdateStatusInput) => {
  const detail = updateCheckedAt
    ? `Scanned ${formatRelativeAge(updateCheckedAt)}`
    : "Never scanned";

  if (updateCheckState === "uncheckable") {
    return {
      color: "secondary" as const,
      detail: updateCheckReason || "This image has no repository digest",
      label: "Cannot check",
    };
  }
  if (updateCheckState === "error" || updateError) {
    return {
      color: "error" as const,
      detail: updateError,
      label: "Scan failed",
    };
  }
  if (updateAvailable === true) {
    return {
      color: "warning" as const,
      detail: updateCheckReason || detail,
      label: "Update available",
    };
  }
  if (updateAvailable === false || updateCheckedAt) {
    return { color: "success" as const, detail, label: "Up to date" };
  }
  return { color: "inherit" as const, detail, label: "Not scanned" };
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
    // Grouping input: a changed project label regroups the rows.
    getComposeProject(container) ?? "",
    container.icon ?? "",
    container.url ?? "",
    container.updateAvailable === undefined
      ? ""
      : String(container.updateAvailable),
    container.updateCheckedAt ?? "",
    container.updateCheckReason ?? "",
    container.updateCheckState ?? "",
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

// Cells only ever mount against container rows — `renderRow` swaps the whole
// row out for stack headers — so the cast is safe there. The render-key metas
// do run against header rows while the cell models are built, so they go
// through the guard below, which hands headers a stable key instead.
const asContainer = (row: unknown) => row as ContainerInfo;

const containerCellRenderKey =
  (getKey: (container: ContainerInfo) => AppDataTableCellRenderKey) =>
  (row: unknown): AppDataTableCellRenderKey =>
    isStackHeaderRow(row) ? [row.project] : getKey(asContainer(row));

const isContainerRowSortable = (
  row: Row<AppTableFeatures, ContainerTableRow>,
) => !isStackHeaderRow(row.original);

const getContainerTableRowId = (row: ContainerTableRow) =>
  isStackHeaderRow(row) ? `stack:${row.project}` : row.Id;

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
        fontWeight={600}
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

interface StackHeaderCellProps {
  header: ContainerStackHeaderRow;
  onToggleStack?: (project: string) => void;
}

// The one cell of a stack header row, spanning every column. The row click
// toggles the stack too; the chevron button is what keyboard users get.
function StackHeaderCell({ header, onToggleStack }: StackHeaderCellProps) {
  const theme = useAppTheme();
  const summary = summarizeStack(header.containers);

  return (
    <div className="app-dt__cell container-table__stack-group-cell" role="cell">
      {header.collapsed ? (
        <DockerIcon
          alt={header.project}
          identifier={header.project.toLowerCase()}
          size={18}
        />
      ) : (
        <Icon
          className="container-table__stack-group-icon"
          height={18}
          icon="mdi:layers-outline"
          width={18}
        />
      )}
      <AppTypography
        fontWeight={600}
        noWrap
        title={header.project}
        toastMeta={DOCKER_TOAST_META}
        variant="body2"
      >
        {header.project}
      </AppTypography>
      <AppTypography color="text.secondary" noWrap variant="caption">
        {formatStackSummary(summary)}
      </AppTypography>
      {/* Expanded stacks show this per row; folded ones surface it here. */}
      {header.collapsed && summary.updateAvailable && (
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
            <Icon aria-hidden icon="mdi:alert" width={14} />
          </span>
        </AppTooltip>
      )}
      <button
        aria-expanded={!header.collapsed}
        aria-label={`${header.collapsed ? "Expand" : "Collapse"} stack ${header.project}`}
        className="container-table__stack-group-toggle"
        onClick={() => onToggleStack?.(header.project)}
        type="button"
      >
        <Icon
          className="container-table__stack-chevron"
          height={18}
          icon="mdi:chevron-down"
          width={18}
        />
      </button>
    </div>
  );
}

function VersionCell({ version }: { version: string }) {
  return (
    <AppTypography
      className="container-table__version-text"
      color="text.secondary"
      noWrap
      title={version}
      toastMeta={DOCKER_TOAST_META}
      variant="body2"
    >
      {version}
    </AppTypography>
  );
}

interface UpdateCellProps {
  containerId: string;
  name: string;
  state: string;
  updateAvailable?: boolean;
  updateCheckedAt?: ContainerInfo["updateCheckedAt"];
  updateCheckReason?: string;
  updateCheckState?: ContainerInfo["updateCheckState"];
  updateError?: string;
}

const UpdateCell = memo(function UpdateCell({
  containerId,
  name,
  state,
  updateAvailable,
  updateCheckedAt,
  updateCheckReason,
  updateCheckState,
  updateError,
}: UpdateCellProps) {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const { isUpdating, startUpdate, updating } = useDockerUpdateOperation();
  const checkingUpdates = useContext(CheckingUpdatesContext);
  const updateStatus = getUpdateStatus({
    updateAvailable,
    updateCheckedAt,
    updateCheckReason,
    updateCheckState,
    updateError,
  });
  const { mutate: checkContainerUpdate, isPending: isCheckingUpdate } =
    useCallMutation(linuxio.docker.check_container_update, {
      success: (result) => {
        const errors = result?.errors ?? 0;
        const updates = result?.updates ?? 0;
        const uncheckable = result?.uncheckable ?? 0;
        if (errors > 0) {
          toast.warning(
            `Failed to check updates for ${name}: ${errors} error(s)`,
          );
          return;
        }
        if (uncheckable > 0) {
          toast.warning(`Cannot check updates for ${name}`);
          return;
        }
        toast.success(
          updates > 0
            ? `Container ${name} has an update`
            : `Container ${name} is up to date`,
        );
      },
      error: `Failed to check updates for ${name}`,
      toast: DOCKER_TOAST_META,
    });
  const isUpdatePending = isUpdating(containerId);

  const checking = isCheckingUpdate || checkingUpdates;

  // An actionable row gets the chip instead of a dot: it is both the amber
  // state and the way to apply the update, and re-checking a container that
  // already has one pending buys nothing.
  if (updateAvailable === true) {
    if (state !== "running") {
      return (
        <Chip
          color="warning"
          label="Available"
          size="small"
          title="Update available; stopped containers are handled by the scheduled update policy"
          variant="soft"
        />
      );
    }
    return (
      <Chip
        color="warning"
        disabled={updating}
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
        onClick={() => startUpdate(containerId, name)}
        size="small"
        title="Apply update"
        variant="soft"
      />
    );
  }

  return (
    <AppTooltip
      title={
        <>
          <div>{updateStatus.detail}</div>
          <div className="container-table__update-hint">
            {checking ? "Scanning…" : "Click to re-scan"}
          </div>
        </>
      }
    >
      <span>
        <AppButton
          aria-label={`Re-scan ${name} for updates`}
          className={[
            "container-table__update-label",
            updateStatus.color === "inherit" &&
              "container-table__update-label--muted",
          ]
            .filter(Boolean)
            .join(" ")}
          color={updateStatus.color}
          disabled={checking}
          onClick={() => checkContainerUpdate({ containerId })}
        >
          {checking ? "Scanning…" : updateStatus.label}
        </AppButton>
      </span>
    </AppTooltip>
  );
});

function UptimeCell({ created }: { created: number }) {
  return (
    <AppTypography
      color="text.secondary"
      style={{ fontVariantNumeric: "tabular-nums" }}
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
      style={{ fontFamily: "var(--app-font-mono)" }}
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
            style={{ fontFamily: "var(--app-font-mono)" }}
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
                style={{ fontFamily: "var(--app-font-mono)" }}
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
            style={{ fontFamily: "var(--app-font-mono)" }}
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
                style={{ fontFamily: "var(--app-font-mono)" }}
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
        style={{ fontVariantNumeric: "tabular-nums" }}
        variant="body2"
      >
        {cpuPercent.toFixed(1)}%
      </AppTypography>
      <AppTypography
        color="text.secondary"
        noWrap
        style={{ fontVariantNumeric: "tabular-nums" }}
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
  // Below md only Name and Actions remain, so the action strip collapses into
  // one menu to keep the name legible.
  compact: boolean;
  containerId: string;
  name: string;
  onOpenLogs: (containerId: string, containerName: string) => void;
  onOpenTerminal: (containerId: string, containerName: string) => void;
  pending?: boolean;
  state: string;
  url?: string;
}

const ActionsCell = memo(function ActionsCell({
  compact,
  containerId,
  name,
  onOpenLogs,
  onOpenTerminal,
  pending = false,
  state,
  url,
}: ActionsCellProps) {
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);
  const { mutate: startContainer, isPending: isStartPending } = useCallMutation(
    linuxio.docker.start_container,
    {
      success: `Container ${name} started`,
      error: `Failed to start ${name}`,
      toast: DOCKER_TOAST_META,
    },
  );
  const { mutate: stopContainer, isPending: isStopPending } = useCallMutation(
    linuxio.docker.stop_container,
    {
      success: `Container ${name} stopped`,
      error: `Failed to stop ${name}`,
      toast: DOCKER_TOAST_META,
    },
  );
  const { mutate: restartContainer, isPending: isRestartPending } =
    useCallMutation(linuxio.docker.restart_container, {
      success: `Container ${name} restarted`,
      error: `Failed to restart ${name}`,
      toast: DOCKER_TOAST_META,
    });
  const { mutate: removeContainer, isPending: isRemovePending } =
    useCallMutation(linuxio.docker.remove_container, {
      success: `Container ${name} removed`,
      error: `Failed to remove ${name}`,
      toast: DOCKER_TOAST_META,
    });
  const rowBusy =
    pending ||
    isStartPending ||
    isStopPending ||
    isRestartPending ||
    isRemovePending;
  const pendingActionLabel =
    pending || isStopPending
      ? "Stopping"
      : isStartPending
        ? "Starting"
        : isRestartPending
          ? "Restarting"
          : isRemovePending
            ? "Removing"
            : undefined;

  const actions: ContainerAction[] = [
    state === "running"
      ? {
          icon: "mdi:stop",
          label: "Stop",
          loading: pending || isStopPending,
          onClick: () => stopContainer({ containerId }),
        }
      : {
          icon: "mdi:play",
          label: "Start",
          loading: isStartPending,
          onClick: () => startContainer({ containerId }),
        },
    {
      icon: "mdi:restart",
      label: "Restart",
      loading: isRestartPending,
      onClick: () => restartContainer({ containerId }),
    },
    {
      icon: "mdi:delete",
      label: "Remove",
      loading: isRemovePending,
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
          ariaLabel={
            pendingActionLabel
              ? `${pendingActionLabel} ${name}`
              : `Actions for ${name}`
          }
          disabled={rowBusy}
          icon="mdi:dots-vertical"
          iconSize={20}
          loading={rowBusy}
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
              disabled={rowBusy}
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
              disabled={rowBusy && !action.loading}
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
  checkingUpdates?: boolean;
  /** Stacks folded down to their header row. Owned by the page, so the fold
   * survives the card/table toggle. */
  collapsedStackIds?: ReadonlySet<string>;
  containers: ContainerInfo[];
  /** Reorder wiring from `useReorderableTableDnd`; omit to lock the row order. */
  dnd?: AppDataTableDndOptions<ContainerTableRow>;
  onSelectContainer?: (containerId: string) => void;
  onToggleStack?: (project: string) => void;
  stoppingContainerIds?: ReadonlySet<string>;
}

interface ContainerDialogTarget {
  id: string;
  name: string;
}

const EMPTY_COLLAPSED_STACK_IDS = new Set<string>();

const ContainerTable = ({
  checkingUpdates = false,
  collapsedStackIds = EMPTY_COLLAPSED_STACK_IDS,
  containers,
  dnd,
  onSelectContainer,
  onToggleStack,
  stoppingContainerIds = EMPTY_STOPPING_CONTAINER_IDS,
}: ContainerTableProps) => {
  const theme = useAppTheme();
  // Same breakpoint the Version and Uptime columns hide at, so the strip
  // collapses exactly when Actions starts crowding the name.
  const compactActions = useAppMediaQuery(theme.breakpoints.down("md"));
  const editMode = dnd?.editing ?? false;
  // Layout mode flattens the grouping: the saved order is the flat list being
  // rearranged, and header rows would sit between drop targets meaning nothing.
  const rows = useMemo<ContainerTableRow[]>(
    () =>
      editMode
        ? containers
        : buildContainerTableRows(containers, collapsedStackIds),
    [collapsedStackIds, containers, editMode],
  );
  const stackAwareDnd = useMemo(
    () => (dnd ? { ...dnd, isRowSortable: isContainerRowSortable } : undefined),
    [dnd],
  );
  const { collapsedStackMemberIds, stackMemberIds } = useMemo(() => {
    const collapsedMemberIds = new Set<string>();
    const memberIds = new Set<string>();
    for (const row of rows) {
      if (isStackHeaderRow(row)) {
        for (const member of row.containers) {
          memberIds.add(member.Id);
          if (row.collapsed) collapsedMemberIds.add(member.Id);
        }
      }
    }
    return {
      collapsedStackMemberIds: collapsedMemberIds,
      stackMemberIds: memberIds,
    };
  }, [rows]);
  const getRowAttributes = useCallback(
    (
      row: Row<AppTableFeatures, ContainerTableRow>,
    ): AppDataTableRowAttributes => ({
      className:
        !isStackHeaderRow(row.original) && stackMemberIds.has(row.original.Id)
          ? "container-table__stack-member-row"
          : undefined,
    }),
    [stackMemberIds],
  );
  const renderRow = useCallback(
    ({
      cells,
      row,
      rowProps,
    }: AppDataTableRowRenderProps<ContainerTableRow>) => {
      const original = row.original;
      if (isStackHeaderRow(original)) {
        return (
          <div
            {...rowProps}
            aria-expanded={!original.collapsed}
            className={[rowProps.className, "container-table__stack-group-row"]
              .filter(Boolean)
              .join(" ")}
          >
            <StackHeaderCell header={original} onToggleStack={onToggleStack} />
          </div>
        );
      }
      if (!stackMemberIds.has(original.Id)) {
        return <div {...rowProps}>{cells}</div>;
      }
      return (
        <AppCollapse
          in={!collapsedStackMemberIds.has(original.Id)}
          unmountOnExit
        >
          <div {...rowProps}>{cells}</div>
        </AppCollapse>
      );
    },
    [collapsedStackMemberIds, onToggleStack, stackMemberIds],
  );
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
    (row: { original: ContainerTableRow }, event: MouseEvent) => {
      // A row is a container of controls before it is a link: the action
      // buttons, the "+N more" expander, the update dot and the copy-on-click
      // cells all bubble up to here and must not navigate.
      if (!clickTargetsRowBody(event.target)) return;
      if (isStackHeaderRow(row.original)) {
        onToggleStack?.(row.original.project);
        return;
      }
      onSelectContainer?.(row.original.Id);
    },
    [onSelectContainer, onToggleStack],
  );
  const columns = useMemo<AppDataTableColumnDef<ContainerTableRow>[]>(
    () => [
      {
        id: "name",
        header: "Name",
        cell: ({ row }) => (
          <ContainerNameCell container={asContainer(row.original)} />
        ),
        meta: {
          align: "left",
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getContainerName(container),
            getDisplayState(container),
            container.icon,
          ]),
          width: "minmax(0, 1.6fr)",
        },
      },
      {
        id: "version",
        header: "Version",
        cell: ({ row }) => {
          const container = asContainer(row.original);
          return (
            <div className="container-table__version-stack">
              <VersionCell version={getVersionDisplay(container)} />
              <UpdateCell
                containerId={container.Id}
                name={getContainerName(container)}
                state={container.State}
                updateAvailable={container.updateAvailable}
                updateCheckedAt={container.updateCheckedAt}
                updateCheckReason={container.updateCheckReason}
                updateCheckState={container.updateCheckState}
                updateError={container.updateError}
              />
            </div>
          );
        },
        meta: {
          align: "center",
          // No `checkingUpdates` here for the same reason the actions column
          // omits the auto-update state: the cell subscribes to it directly.
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getVersionDisplay(container),
            getContainerName(container),
            container.updateAvailable,
            container.updateCheckedAt,
            container.updateCheckReason,
            container.updateCheckState,
            container.updateError,
          ]),
          hideBelow: "md",
          width: "170px",
        },
      },
      {
        id: "uptime",
        header: "Uptime",
        cell: ({ row }) => (
          <UptimeCell created={asContainer(row.original).Created} />
        ),
        meta: { hideBelow: "md", width: "90px" },
      },
      {
        id: "network",
        header: "Network",
        cell: ({ row }) => (
          <NetworkCell
            networks={Object.entries(
              asContainer(row.original).NetworkSettings?.Networks ?? {},
            )}
          />
        ),
        meta: {
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            Object.keys(container.NetworkSettings?.Networks ?? {})
              .sort()
              .join("|"),
          ]),
          hideBelow: "lg",
        },
      },
      {
        id: "ip",
        header: "Container IP",
        cell: ({ row }) => (
          <NetworkAddressCell
            networks={Object.entries(
              asContainer(row.original).NetworkSettings?.Networks ?? {},
            )}
          />
        ),
        meta: {
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            Object.entries(container.NetworkSettings?.Networks ?? {})
              .sort(([left], [right]) => left.localeCompare(right))
              .map(
                ([networkName, endpoint]) =>
                  `${networkName}:${endpoint.IPAddress || "-"}`,
              )
              .join("|"),
          ]),
          hideBelow: "lg",
          width: "130px",
        },
      },
      {
        id: "ports",
        header: () => (
          <AppTooltip placement="top" title="Container -> Host">
            <span>Ports</span>
          </AppTooltip>
        ),
        cell: ({ row }) => {
          const container = asContainer(row.original);
          return (
            <PortsCell
              containerId={container.Id}
              onToggleExpanded={toggleExpanded}
              ports={getDedupedPorts(container)}
            />
          );
        },
        meta: {
          hideBelow: "xl",
          width: "minmax(130px, 155px)",
          cellStyle: { alignItems: "flex-start" },
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getDedupedPorts(container)
              .map(
                (port) =>
                  `${port.PrivatePort}:${port.PublicPort ?? "-"}:${port.Type}`,
              )
              .join("|"),
          ]),
        },
      },
      {
        id: "volumes",
        header: () => (
          <AppTooltip placement="top" title="App -> Host">
            <span>Volumes</span>
          </AppTooltip>
        ),
        cell: ({ row }) => {
          const container = asContainer(row.original);
          return (
            <VolumesCell
              containerId={container.Id}
              mounts={getMounts(container)}
              onToggleExpanded={toggleExpanded}
            />
          );
        },
        meta: {
          hideBelow: "xl",
          width: "minmax(0, 2.2fr)",
          cellStyle: { alignItems: "flex-start" },
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getMounts(container)
              .map(
                (mount) => `${mount.Type}:${mount.Destination}:${mount.Source}`,
              )
              .join("|"),
          ]),
        },
      },
      {
        id: "metrics",
        header: "CPU / Mem",
        cell: ({ row }) => (
          <MetricsCell container={asContainer(row.original)} />
        ),
        meta: {
          align: "center",
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            (container.metrics?.cpu_percent ?? 0).toFixed(1),
            formatFileSize(container.metrics?.mem_usage ?? 0),
          ]),
          hideBelow: "xl",
          width: "110px",
        },
      },
      {
        id: "actions",
        header: "Actions",
        enableSorting: false,
        cell: ({ row }) => {
          const container = asContainer(row.original);
          const name = getContainerName(container);
          return (
            <ActionsCell
              compact={compactActions}
              containerId={container.Id}
              name={name}
              onOpenLogs={openLogs}
              onOpenTerminal={openTerminal}
              pending={stoppingContainerIds.has(container.Id)}
              state={container.State}
              url={container.url}
            />
          );
        },
        meta: {
          align: "right",
          // The buttons fill the column, so a right-aligned label reads as
          // hanging off the end of the strip rather than titling it.
          headerStyle: { justifyContent: "center" },
          // Tighter than the default 16px so the action strip fits the track.
          cellStyle: { gap: 2, paddingInline: 8 },
          // A compact row holds nothing but the menu button, so the rest of the
          // track goes back to the name.
          width: compactActions ? "56px" : "215px",
          getCellRenderKey: containerCellRenderKey((container) => [
            container.Id,
            getContainerName(container),
            container.State,
            container.url,
            stoppingContainerIds.has(container.Id),
          ]),
        },
      },
    ],
    [
      compactActions,
      openLogs,
      openTerminal,
      stoppingContainerIds,
      toggleExpanded,
    ],
  );
  return (
    <>
      <ExpandedContainersContext.Provider value={expandedContainerIds}>
        <CheckingUpdatesContext.Provider value={checkingUpdates}>
          <AppDataTable
            ariaLabel="Docker containers"
            columns={columns}
            data={rows}
            dnd={stackAwareDnd}
            emptyMessage="No containers found."
            enableSorting={false}
            fillAvailable
            getRowAttributes={getRowAttributes}
            getRowId={getContainerTableRowId}
            // Dragging rows is the point of edit mode; selecting one there
            // would fight the drag and immediately swap the table for a
            // detail view.
            onRowClick={
              (onSelectContainer || onToggleStack) && !editMode
                ? handleRowClick
                : undefined
            }
            renderRow={renderRow}
          />
        </CheckingUpdatesContext.Provider>
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
  previous.checkingUpdates === next.checkingUpdates &&
  previous.dnd === next.dnd &&
  previous.onSelectContainer === next.onSelectContainer &&
  previous.onToggleStack === next.onToggleStack &&
  areStringSetsEqual(
    previous.collapsedStackIds ?? EMPTY_COLLAPSED_STACK_IDS,
    next.collapsedStackIds ?? EMPTY_COLLAPSED_STACK_IDS,
  ) &&
  areStringSetsEqual(
    previous.stoppingContainerIds ?? EMPTY_STOPPING_CONTAINER_IDS,
    next.stoppingContainerIds ?? EMPTY_STOPPING_CONTAINER_IDS,
  ) &&
  areContainerArraysEquivalent(previous.containers, next.containers);

export default memo(ContainerTable, areContainerTablePropsEqual);
