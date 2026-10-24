import { Icon } from "@iconify/react";
import { createContext, memo, useContext, type ReactNode } from "react";

import {
  linuxio,
  type ContainerEndpoint,
  type ContainerInfo,
  type ContainerMount,
  type ContainerPort,
  useCallMutation,
} from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { getContainerStatusColor } from "@/constants/statusColors";
import { useScopedToast } from "@/hooks/useScopedToast";
import {
  formatContainerUptime,
  getContainerDisplayState,
  getContainerName,
} from "@/utils/dockerContainer";
import { formatFileSize } from "@/utils/formatters";

import ContainerActions from "./ContainerActions";
import {
  formatStackSummary,
  summarizeStack,
  type ContainerStackHeaderRow,
} from "./containerStacks";
import {
  getUpdateStatus,
  getVisibleEntries,
  isCollapsible,
} from "./containerTableModel";
import { DOCKER_TOAST_META } from "../shared/dockerToast";
import { useDockerUpdateOperation } from "../shared/DockerUpdateOperationProvider";

// Expanded-row state is shared with the cells via context rather than baked into
// the column definitions. flexRender renders a cell function as a component, so
// rebuilding the columns on every toggle would change each cell's component
// identity and remount its subtree — which restarts AppCollapse already-open and
// skips the expand animation. Context lets the columns stay stable while cells
// still re-render in place when the expanded set changes.
export const ExpandedContainersContext = createContext<ReadonlySet<string>>(
  new Set(),
);

// And so does the page-wide "checking updates" flag, which toggles twice per
// check — once on click, once when the sweep returns, the second one landing
// long after the pointer has moved back over the rows.
export const CheckingUpdatesContext = createContext(false);

export function ContainerNameCell({ container }: { container: ContainerInfo }) {
  const name = getContainerName(container);
  const displayState = getContainerDisplayState(container);

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
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
  dragHandle?: ReactNode;
  header: ContainerStackHeaderRow;
  onToggleStack?: (project: string) => void;
}

// The one cell of a stack header row, spanning every column. The row click
// toggles the stack too; the chevron button is what keyboard users get.
export function StackHeaderCell({
  dragHandle,
  header,
  onToggleStack,
}: StackHeaderCellProps) {
  const summary = summarizeStack(header.containers);

  return (
    <div className="app-dt__cell container-table__stack-group-cell" role="cell">
      {dragHandle && (
        <span className="container-table__stack-group-drag">{dragHandle}</span>
      )}
      {/* The stack glyph fills the member row's status-dot slot; its project
          image then stays in the same position as every container icon. */}
      <Icon
        className="container-table__stack-group-icon"
        height={14}
        icon="mdi:layers-outline"
        width={14}
      />
      <DockerIcon
        alt={header.project}
        identifier={header.project.toLowerCase()}
        size={24}
      />
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
              color: "var(--app-palette-warning-main)",
              display: "flex",
            }}
          >
            <Icon aria-hidden icon="mdi:alert" width={14} />
          </span>
        </AppTooltip>
      )}
      <AppIconButton
        aria-expanded={!header.collapsed}
        aria-label={`${header.collapsed ? "Expand" : "Collapse"} stack ${header.project}`}
        className="container-table__stack-group-toggle"
        color="secondary"
        onClick={() => onToggleStack?.(header.project)}
        size="small"
      >
        <Icon
          className="container-table__stack-chevron"
          height={18}
          icon="mdi:chevron-down"
          width={18}
        />
      </AppIconButton>
    </div>
  );
}

export function VersionCell({ version }: { version: string }) {
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

export const UpdateCell = memo(function UpdateCell({
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

export function UptimeCell({ created }: { created: number }) {
  return (
    <AppTypography
      color="text.secondary"
      style={{ fontVariantNumeric: "tabular-nums" }}
      variant="body2"
    >
      {formatContainerUptime(created)}
    </AppTypography>
  );
}

export function NetworkCell({
  networks,
}: {
  networks: Array<[string, ContainerEndpoint]>;
}) {
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
            color: "var(--app-palette-text-disabled)",
          }}
        >
          +{networks.length - 1}
        </span>
      )}
    </AppTypography>
  );
}

export function NetworkAddressCell({
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

export function PortsCell({
  containerId,
  onToggleExpanded,
  ports,
}: PortsCellProps) {
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
            <span style={{ color: "var(--app-palette-text-primary)" }}>
              {port.PrivatePort}/{port.Type}
            </span>
            <span
              style={{
                color: "var(--app-palette-text-disabled)",
                marginInline: 2,
              }}
            >
              {"->"}
            </span>
            <span style={{ color: "var(--app-palette-text-secondary)" }}>
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
                <span style={{ color: "var(--app-palette-text-primary)" }}>
                  {port.PrivatePort}/{port.Type}
                </span>
                <span
                  style={{
                    color: "var(--app-palette-text-disabled)",
                    marginInline: 2,
                  }}
                >
                  {"->"}
                </span>
                <span style={{ color: "var(--app-palette-text-secondary)" }}>
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

export function VolumesCell({
  containerId,
  mounts,
  onToggleExpanded,
}: VolumesCellProps) {
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
            <span style={{ color: "var(--app-palette-text-primary)" }}>
              {mount.Destination}
            </span>
            <span
              style={{
                color: "var(--app-palette-text-disabled)",
                marginInline: 2,
              }}
            >
              {"->"}
            </span>
            <span style={{ color: "var(--app-palette-text-secondary)" }}>
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
                <span style={{ color: "var(--app-palette-text-primary)" }}>
                  {mount.Destination}
                </span>
                <span
                  style={{
                    color: "var(--app-palette-text-disabled)",
                    marginInline: 2,
                  }}
                >
                  {"->"}
                </span>
                <span style={{ color: "var(--app-palette-text-secondary)" }}>
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

export function MetricsCell({ container }: { container: ContainerInfo }) {
  const cpuPercent = container.metrics?.cpu_percent;
  const memUsage = container.metrics?.memory_usage_bytes;

  return (
    <div style={{ display: "flex", flexDirection: "column" }}>
      <AppTypography
        color="text.secondary"
        noWrap
        style={{ fontVariantNumeric: "tabular-nums" }}
        variant="body2"
      >
        {cpuPercent === undefined ? "—" : `${cpuPercent.toFixed(1)}%`}
      </AppTypography>
      <AppTypography
        color="text.secondary"
        noWrap
        style={{ fontVariantNumeric: "tabular-nums" }}
        variant="body2"
      >
        {memUsage === undefined ? "—" : formatFileSize(memUsage)}
      </AppTypography>
    </div>
  );
}

interface ActionsCellProps {
  // Below md only Name and Actions remain, so the action strip collapses into
  // one menu to keep the name legible.
  compact: boolean;
  containerId: string;
  labels?: Record<string, string>;
  name: string;
  onOpenLogs: (containerId: string, containerName: string) => void;
  onOpenTerminal: (containerId: string, containerName: string) => void;
  pending?: boolean;
  state: string;
  url?: string;
}

export const ActionsCell = memo(function ActionsCell({
  compact,
  containerId,
  labels,
  name,
  onOpenLogs,
  onOpenTerminal,
  pending = false,
  state,
  url,
}: ActionsCellProps) {
  return (
    <ContainerActions
      actionPending={pending}
      container={{ Id: containerId, Labels: labels, State: state, url }}
      mode={compact ? "menu" : "icons"}
      name={name}
      onOpenLogs={() => onOpenLogs(containerId, name)}
      onOpenTerminal={() => onOpenTerminal(containerId, name)}
    />
  );
});
