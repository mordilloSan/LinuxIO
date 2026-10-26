import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState, type CSSProperties, type ReactNode } from "react";

import {
  linuxio,
  useCallMutation,
  type BtrfsFilesystem,
  type BtrfsOfflinePool,
  type BtrfsOpProgress,
  type FilesystemInfo,
  type NFSMount,
  type StorageBlockDevice,
  type TaskProgress,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
import AppTextField from "@/components/ui/AppTextField";
import AppTopologyEdge from "@/components/ui/AppTopologyEdge";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import useAuth from "@/hooks/useAuth";
import { useCapability } from "@/hooks/useCapabilities";
import useContainerWidth from "@/hooks/useContainerWidth";
import { DASHBOARD_CARD_SPACING } from "@/theme/constants";
import { formatFileSize, formatThroughput } from "@/utils/formatters";

import { driveIcon } from "./DriveCard";
import MdRaidDetails from "./MdRaidDetails";
import {
  buildStorageGraph,
  deviceTypeLabel,
  fsTypeLabel,
  type GraphEdge,
  type GraphNode,
  type PoolSummary,
} from "./storageGraph";
import type { DriveInfo } from "./types";
import { getHealthColor, getTemperature, getTemperatureColor } from "./utils";
import BtrfsCreatePoolDialog from "../btrfs/BtrfsCreatePoolDialog";
import BtrfsDevicesCard from "../btrfs/BtrfsDevicesCard";
import BtrfsHealthBanner from "../btrfs/BtrfsHealthBanner";
import BtrfsSettingsCard from "../btrfs/BtrfsSettingsCard";
import BtrfsSpaceCard from "../btrfs/BtrfsSpaceCard";
import "./storage-pools.css";

/** Horizontal room between columns, where the edges run. */
const GAP = 96;
/** Room above the nodes for the column titles. */
const HEADER = 28;
/** Largest corner on an edge; smaller when the vertical run is short. */
const CORNER = 40;
const FALLBACK_WIDTH = 1200;

/**
 * From a source exit to a target entry through the lane at `laneX`: a
 * horizontal run, a quarter-curve into the lane, along it, and out again.
 * A short vertical run makes the two curves one S-bend.
 */
const edgePath = (
  x0: number,
  y0: number,
  x1: number,
  y1: number,
  laneX: number,
) => {
  if (Math.abs(y1 - y0) < 1) return `M${x0} ${y0}H${x1}`;
  const r = Math.min(
    CORNER,
    Math.abs(y1 - y0) / 2,
    (laneX - x0) / 2,
    (x1 - laneX) / 2,
  );
  const s = y1 > y0 ? 1 : -1;
  return (
    `M${x0} ${y0}H${laneX - r}Q${laneX} ${y0} ${laneX} ${y0 + s * r}` +
    `V${y1 - s * r}Q${laneX} ${y1} ${laneX + r} ${y1}H${x1}`
  );
};

const HEALTH = {
  success: { color: "var(--app-palette-success-main)", label: "Healthy" },
  warning: { color: "var(--app-palette-warning-main)", label: "Warning" },
  error: { color: "var(--app-palette-error-main)", label: "Failing" },
  default: {
    color: "var(--app-palette-text-disabled)",
    label: "No SMART data",
  },
} as const;

/** One drive: name, model, live rates, unused partitions, size and temperature. */
const DriveCard = ({
  drive,
  unused,
  idle,
  onInspectDrive,
}: {
  drive: DriveInfo;
  unused: StorageBlockDevice[];
  /** Nothing mounts, builds on or sits on the drive. */
  idle: boolean;
  onInspectDrive: (name: string) => void;
}) => {
  const temperature = getTemperature(drive.smart);
  const health = HEALTH[getHealthColor(drive.smart)];
  return (
    <AppButton
      aria-label={`Drive details ${drive.name}`}
      className="storage-pools__disk storage-pools__disk--link"
      color="inherit"
      onClick={() => onInspectDrive(drive.name)}
    >
      <Icon
        className="storage-pools__icon"
        height={24}
        icon={driveIcon(drive.transport, drive.rotational)}
        width={24}
      />
      <StatusDot
        color={health.color}
        size={8}
        style={{ marginLeft: -6 }}
        tooltip={health.label}
      />
      <span className="storage-pools__name">
        <AppTypography noWrap variant="body2">
          {drive.name}
        </AppTypography>
        <AppTypography color="text.secondary" noWrap variant="caption">
          {drive.model}
        </AppTypography>
        {drive.rates && (
          <AppTypography color="text.secondary" noWrap variant="caption">
            Read {formatThroughput(drive.rates.read_bytes_per_sec)} · Write{" "}
            {formatThroughput(drive.rates.write_bytes_per_sec)}
          </AppTypography>
        )}
        {idle && (
          <AppTypography color="text.secondary" noWrap variant="caption">
            Nothing on it
          </AppTypography>
        )}
        {unused.map((partition) => (
          <AppTypography
            className="storage-pools__unused"
            color="text.disabled"
            key={partition.path}
            noWrap
            title={`${partition.name} is not mounted`}
            variant="caption"
          >
            {partition.name} ·{" "}
            {partition.fsType ? fsTypeLabel(partition.fsType) : "No filesystem"}{" "}
            · {formatFileSize(partition.sizeBytes)} · not mounted
          </AppTypography>
        ))}
      </span>
      <AppTypography color="text.secondary" variant="caption">
        {formatFileSize(drive.sizeBytes)}
      </AppTypography>
      {/* No tooltip here: a tooltip trigger inside the button eats its click. */}
      {temperature !== null && (
        <AppTypography
          color={getTemperatureColor(temperature)}
          fontWeight={600}
          variant="caption"
        >
          {temperature}°C
        </AppTypography>
      )}
    </AppButton>
  );
};

/** A mounted filesystem; opens its filesystem card. */
const VolumeCard = ({
  volume,
  onOpenVolume,
}: {
  volume: FilesystemInfo;
  onOpenVolume: (mountpoint: string) => void;
}) => (
  <AppButton
    aria-label={`Volume ${volume.mountpoint}`}
    className="storage-pools__card storage-pools__volume"
    color="inherit"
    onClick={() => onOpenVolume(volume.mountpoint)}
  >
    <Icon
      className="storage-pools__icon"
      height={26}
      icon="mdi:chart-pie"
      width={26}
    />
    <span>
      <AppTypography fontWeight={600} noWrap variant="body2">
        {volume.mountpoint}
      </AppTypography>
      <AppTypography color="text.secondary" noWrap variant="caption">
        {volume.device.replace(/^\/dev\//, "")}
      </AppTypography>
    </span>
    <AppChip
      color="primary"
      label={fsTypeLabel(volume.fstype)}
      size="small"
      variant="soft"
    />
    <AppTypography color="text.secondary" variant="caption">
      Free {formatFileSize(volume.free)}
    </AppTypography>
  </AppButton>
);

/** A btrfs pool; opens its details below the graph. */
const PoolCard = ({
  pool,
  selected,
  onSelectPool,
}: {
  pool: PoolSummary;
  selected: boolean;
  onSelectPool: (uuid?: string) => void;
}) => (
  <AppButton
    aria-label={pool.name}
    aria-pressed={selected}
    className="storage-pools__card storage-pools__pool"
    color="inherit"
    onClick={() => onSelectPool(selected ? undefined : pool.uuid)}
  >
    <Icon
      className="storage-pools__icon"
      height={36}
      icon="mdi:database-outline"
      width={36}
    />
    <AppTypography fontWeight={600} noWrap variant="body2">
      {pool.name}
    </AppTypography>
    {pool.profile && (
      <AppChip
        color="primary"
        label={pool.profile}
        size="small"
        variant="soft"
      />
    )}
    <AppTypography color="text.secondary" variant="caption">
      Allocated {formatFileSize(pool.allocatedBytes)} · Free{" "}
      {formatFileSize(pool.freeBytes)}
    </AppTypography>
  </AppButton>
);

/** A btrfs pool that is not mounted, with Mount and Destroy. */
const OfflinePoolCard = ({
  pool,
  missing,
  isMounting,
  onMount,
  onDestroy,
}: {
  pool: BtrfsOfflinePool;
  missing: number;
  isMounting: boolean;
  onMount: () => void;
  onDestroy: () => void;
}) => (
  <div className="storage-pools__card storage-pools__pool">
    <Icon
      className="storage-pools__icon"
      height={36}
      icon="mdi:database-off-outline"
      width={36}
    />
    <AppTypography fontWeight={600} noWrap variant="body2">
      {pool.label || pool.uuid}
    </AppTypography>
    <AppChip
      color={missing > 0 ? "error" : "warning"}
      label={missing > 0 ? "Degraded" : "Not mounted"}
      size="small"
      variant="soft"
    />
    <AppTypography color="text.secondary" variant="caption">
      {pool.mountpoint ? `fstab: ${pool.mountpoint}` : "Not in /etc/fstab"}
    </AppTypography>
    <div style={{ display: "flex", gap: 4 }}>
      <AppButton
        disabled={isMounting}
        onClick={onMount}
        size="small"
        variant="outlined"
      >
        {missing > 0 ? "Mount degraded" : "Mount"}
      </AppButton>
      <AppButton color="error" onClick={onDestroy} size="small">
        Destroy
      </AppButton>
    </div>
  </div>
);

/**
 * A RAID, LVM or mapper device over its drives; an md array opens its
 * details, the others are not managed here.
 */
const DeviceCard = ({
  node,
  onSelectRaid,
  selectedRaid,
}: {
  node: Extract<GraphNode, { kind: "device" }>;
  onSelectRaid: (name?: string) => void;
  selectedRaid?: string;
}) => {
  const { array, caption, label, sizeBytes, type } = node;
  const content = (
    <>
      <Icon
        className="storage-pools__icon"
        height={36}
        icon="mdi:layers-outline"
        width={36}
      />
      <AppTypography fontWeight={600} noWrap variant="body2">
        {label}
      </AppTypography>
      <span style={{ display: "flex", gap: 4 }}>
        <AppChip
          color="primary"
          label={deviceTypeLabel(type)}
          size="small"
          variant="soft"
        />
        {array && array.degraded > 0 && (
          <AppChip color="error" label="Degraded" size="small" variant="soft" />
        )}
      </span>
      <AppTypography color="text.secondary" variant="caption">
        {caption ? `${caption} · ` : ""}
        {formatFileSize(sizeBytes)}
      </AppTypography>
    </>
  );
  if (!array) {
    return (
      <div className="storage-pools__card storage-pools__pool">{content}</div>
    );
  }
  return (
    <AppButton
      aria-label={label}
      aria-pressed={array.name === selectedRaid}
      className="storage-pools__card storage-pools__pool"
      color="inherit"
      onClick={() =>
        onSelectRaid(array.name === selectedRaid ? undefined : array.name)
      }
    >
      {content}
    </AppButton>
  );
};

/** The EFI System Partitions as one boot set, as the firmware sees them. */
const BootCard = ({ node }: { node: Extract<GraphNode, { kind: "boot" }> }) => {
  const { copies, entries, firmware, mounted } = node;
  const status = !firmware
    ? { color: "default" as const, label: "No firmware entries" }
    : entries >= copies.length
      ? { color: "success" as const, label: "All bootable" }
      : {
          color: "warning" as const,
          label: `${copies.length - entries} without entry`,
        };
  return (
    <div className="storage-pools__card storage-pools__pool">
      <Icon
        className="storage-pools__icon"
        height={36}
        icon="mdi:power"
        width={36}
      />
      <AppTypography fontWeight={600} noWrap variant="body2">
        EFI boot
      </AppTypography>
      <span style={{ display: "flex", gap: 4 }}>
        {firmware && (
          <AppChip
            color="primary"
            label={`${entries} boot ${entries === 1 ? "entry" : "entries"}`}
            size="small"
            variant="soft"
          />
        )}
        <AppChip
          color={status.color}
          label={status.label}
          size="small"
          variant="soft"
        />
      </span>
      <AppTypography color="text.secondary" variant="caption">
        {copies.length} copies ·{" "}
        {mounted ? `${mounted.name} mounted` : "none mounted"}
      </AppTypography>
    </div>
  );
};

/** A network share or a device with no drive found under it. */
const ServerCard = ({
  label,
  network,
}: {
  label: string;
  network: boolean;
}) => (
  <div className="storage-pools__card storage-pools__pool">
    <Icon
      className="storage-pools__icon"
      height={36}
      icon={network ? "mdi:server-network" : "mdi:layers-outline"}
      width={36}
    />
    <AppTypography fontWeight={600} noWrap variant="body2">
      {label}
    </AppTypography>
    <AppTypography color="text.secondary" variant="caption">
      {network ? "Network share" : "No physical disk found"}
    </AppTypography>
  </div>
);

const MissingCard = ({ count }: { count: number }) => (
  <div className="storage-pools__card storage-pools__disk">
    <Icon
      className="storage-pools__icon"
      height={24}
      icon="mdi:harddisk-remove"
      width={24}
    />
    <span className="storage-pools__name">
      <AppTypography noWrap variant="body2">
        {count} missing disk{count > 1 ? "s" : ""}
      </AppTypography>
    </span>
    <AppChip color="error" label="Missing" size="small" variant="soft" />
  </div>
);

/**
 * The connections, in one SVG over the graph: each edge runs from its
 * source's right edge into its target's lane and out into the target. Edges
 * into the same node share the lane. Labels name the partition once per
 * node when every edge into it agrees, else at each edge's source.
 */
const Edges = ({
  edges,
  nodes,
  columnX,
  columnWidth,
  paused,
  highlight,
}: {
  edges: GraphEdge[];
  nodes: GraphNode[];
  columnX: (column: 0 | 1 | 2) => number;
  columnWidth: number;
  paused: boolean;
  highlight: (node: GraphNode) => boolean;
}) => {
  const byId = new Map(nodes.map((node) => [node.id, node]));
  const lanes = nodes.filter((node) => node.column === 1).length;
  // A drive → volume line drops in the first gap, in a lane of its own
  // after the nodes', and crosses the middle column level with its volume,
  // where the layout kept a band free.
  const passes = edges.some(
    (edge) =>
      byId.get(edge.from)!.column === 0 && byId.get(edge.to)!.column === 2,
  )
    ? 1
    : 0;
  const laneSpacing = Math.min(30, (GAP * 2) / 3 / Math.max(lanes + passes, 1));
  const firstLane = columnX(1) - GAP + GAP / 3;
  const laneX = (from: GraphNode, to: GraphNode) => {
    if (to.column === 1) return firstLane + to.lane * laneSpacing;
    if (from.column === 0) return firstLane + lanes * laneSpacing;
    return columnX(2) - GAP / 2;
  };
  const centre = (node: GraphNode) => HEADER + node.top + node.height / 2;
  const entryLabel = new Map<string, string>();
  for (const node of nodes) {
    const labels = edges.filter((e) => e.to === node.id).map((e) => e.label);
    if (
      labels.length > 0 &&
      labels[0] !== "" &&
      labels.every((l) => l === labels[0])
    )
      entryLabel.set(node.id, labels[0]);
  }
  return (
    <>
      {edges.map((edge) => {
        const from = byId.get(edge.from)!;
        const to = byId.get(edge.to)!;
        const x0 = columnX(from.column) + columnWidth;
        const y0 = centre(from) + edge.exit;
        const x1 = columnX(to.column);
        const y1 = centre(to);
        return (
          <AppTopologyEdge
            className="storage-pools__line"
            d={edgePath(x0, y0, x1, y1, laneX(from, to))}
            forward={edge.rates?.read_bytes_per_sec}
            highlighted={highlight(from) || highlight(to)}
            key={edge.id}
            live={edge.live}
            paused={paused}
            reverse={edge.rates?.write_bytes_per_sec}
          />
        );
      })}
      {nodes.flatMap((node) => {
        const label = entryLabel.get(node.id);
        if (label !== undefined)
          return (
            <text
              className="storage-pools__label"
              key={node.id}
              textAnchor="end"
              x={columnX(node.column) - 8}
              y={centre(node) - 5}
            >
              {label}
            </text>
          );
        return edges
          .filter((edge) => edge.to === node.id && edge.label !== "")
          .map((edge) => {
            const from = byId.get(edge.from)!;
            return (
              <text
                className="storage-pools__label"
                key={edge.id}
                x={columnX(from.column) + columnWidth + 6}
                y={centre(from) + edge.exit - 4}
              >
                {edge.label}
              </text>
            );
          });
      })}
    </>
  );
};

type PoolDialog =
  | { kind: "create" }
  | { kind: "destroy"; uuid: string; name: string; disks: string[] }
  | { kind: "rename"; uuid: string; label: string };

interface StoragePoolsProps {
  drives: DriveInfo[];
  filesystems: FilesystemInfo[];
  nfsMounts: NFSMount[];
  onInspectDrive: (name: string) => void;
  onOpenVolume: (mountpoint: string) => void;
  /** UUID of the pool whose details are open; kept in the URL for alert links. */
  selectedPool?: string;
  onSelectPool: (uuid?: string) => void;
  /** Kernel name of the md RAID array whose details are open, e.g. md127. */
  selectedRaid?: string;
  onSelectRaid: (name?: string) => void;
}

/**
 * Every drive once on the left, what the drives build in the middle (btrfs
 * pools, md arrays, LVM and dm-crypt devices, the EFI boot set, network
 * servers) and every mounted filesystem on the right, joined by edges that
 * carry live reads and writes. The selected pool's health, disk and RAID
 * management and settings, or the selected md array's members and scrub, sit
 * below. Pools need btrfs and privilege; the rest does not.
 */
const StoragePools = ({
  drives,
  filesystems,
  nfsMounts,
  onInspectDrive,
  onOpenVolume,
  selectedPool,
  onSelectPool,
  selectedRaid,
  onSelectRaid,
}: StoragePoolsProps) => {
  const { privileged } = useAuth();
  const { isEnabled } = useCapability("btrfsAvailable");
  const poolsEnabled = privileged && isEnabled;
  const { data: btrfs = [], isPending } = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: poolsEnabled,
    refetchInterval: 10000,
  });
  const { data: offline = [] } = useQuery({
    ...linuxio.storage.btrfs_pools_offline,
    enabled: poolsEnabled,
    refetchInterval: 10000,
  });
  const { data: topology, isPending: isTopologyPending } = useQuery(
    linuxio.storage.get_topology,
  );
  const [dialog, setDialog] = useState<PoolDialog | null>(null);
  const [paused, setPaused] = useState(false);
  const { ref: graphRef, width } = useContainerWidth(FALLBACK_WIDTH);
  const closeDialog = () => setDialog(null);
  // A task, not a call: the bridge trims every disk before mkfs, which can
  // outlast any request timeout on large SSDs.
  const [createStep, setCreateStep] = useState<BtrfsOpProgress | null>(null);
  const createPool = linuxio.storage.btrfs_pool_create.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >({
    onProgress: (progress) => setCreateStep(progress.detail ?? null),
    success: "Pool created",
    error: "Failed to create pool",
    options: { onSettled: () => setCreateStep(null) },
  });
  const { mutate: mountPool, isPending: isMounting } = useCallMutation(
    linuxio.storage.btrfs_pool_mount,
    { success: "Pool mounted", error: "Failed to mount pool" },
  );
  const { mutate: destroyPool, isPending: isDestroying } = useCallMutation(
    linuxio.storage.btrfs_pool_destroy,
    { success: "Pool destroyed", error: "Failed to destroy pool" },
  );
  const { mutate: renamePool, isPending: isRenaming } = useCallMutation(
    linuxio.storage.btrfs_set_label,
    { success: "Pool renamed", error: "Failed to rename pool" },
  );
  const hasArrays = (topology?.devices ?? []).some((device) =>
    device.type.startsWith("raid"),
  );
  const { data: arrays = [] } = useQuery({
    ...linuxio.storage.md_list,
    enabled: hasArrays,
    refetchInterval: 5000,
  });

  const graph = buildStorageGraph({
    arrays,
    bootEntries: topology?.bootEntries ?? [],
    btrfs,
    devices: topology?.devices ?? [],
    drives,
    filesystems,
    nfsMounts,
    offline,
  });
  const selected = graph.nodes.find(
    (node): node is Extract<GraphNode, { kind: "pool" }> =>
      node.kind === "pool" && node.fs.uuid === selectedPool,
  );
  const selectedArray = arrays.find(({ name }) => name === selectedRaid);
  const columnWidth = (width - 2 * GAP) / 3;
  const columnX = (column: 0 | 1 | 2) => column * (columnWidth + GAP);
  const hasBoot = graph.nodes.some((node) => node.kind === "boot");
  const highlight = (node: GraphNode) =>
    (node.kind === "pool" && node.fs.uuid === selectedPool) ||
    (node.kind === "device" && node.array?.name === selectedRaid);

  // Until the pools and block-device tree load, volumes would sit under the
  // wrong drive or source and then jump; draw nothing rather than that flicker.
  if ((poolsEnabled && isPending) || isTopologyPending) return null;

  if (drives.length === 0 && filesystems.length === 0 && btrfs.length === 0) {
    return (
      <AppTypography color="text.secondary" style={{ marginBottom: 16 }}>
        No drives or volumes found.
      </AppTypography>
    );
  }

  const card = (node: GraphNode): ReactNode => {
    switch (node.kind) {
      case "drive":
        return (
          <DriveCard
            drive={node.drive}
            idle={
              node.unused.length === 0 &&
              !graph.edges.some((edge) => edge.from === node.id)
            }
            onInspectDrive={onInspectDrive}
            unused={node.unused}
          />
        );
      case "missing":
        return <MissingCard count={node.count} />;
      case "pool":
        return (
          <PoolCard
            onSelectPool={onSelectPool}
            pool={node.pool}
            selected={node.fs.uuid === selectedPool}
          />
        );
      case "offlinePool":
        return (
          <OfflinePoolCard
            isMounting={isMounting}
            missing={node.missing}
            onDestroy={() =>
              setDialog({
                kind: "destroy",
                uuid: node.pool.uuid,
                name: node.pool.label || node.pool.uuid,
                disks: node.pool.devices,
              })
            }
            onMount={() =>
              mountPool({ uuid: node.pool.uuid, degraded: node.missing > 0 })
            }
            pool={node.pool}
          />
        );
      case "device":
        return (
          <DeviceCard
            node={node}
            onSelectRaid={onSelectRaid}
            selectedRaid={selectedRaid}
          />
        );
      case "server":
        return <ServerCard label={node.label} network={node.network} />;
      case "boot":
        return <BootCard node={node} />;
      case "volume":
        return <VolumeCard onOpenVolume={onOpenVolume} volume={node.volume} />;
    }
  };

  return (
    <>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 8,
        }}
      >
        <AppTypography style={{ fontWeight: 600 }} variant="h6">
          Storage space
        </AppTypography>
        <div style={{ display: "flex", gap: 8 }}>
          <AppButton
            aria-pressed={paused}
            color="inherit"
            onClick={() => setPaused(!paused)}
            size="small"
            variant="outlined"
          >
            {paused ? "Resume animation" : "Pause animation"}
          </AppButton>
          {poolsEnabled && (
            <AppButton
              onClick={() => setDialog({ kind: "create" })}
              size="small"
              variant="outlined"
            >
              Create pool
            </AppButton>
          )}
        </div>
      </div>
      <div
        className="storage-pools"
        ref={graphRef}
        style={
          { "--graph-height": `${HEADER + graph.height}px` } as CSSProperties
        }
      >
        {(
          [
            [0, "Drives"],
            [1, hasBoot ? "Boot, pools and arrays" : "Pools and arrays"],
            [2, "Volumes"],
          ] as const
        ).map(([column, title]) => (
          <AppTypography
            className="storage-pools__header"
            color="text.secondary"
            key={column}
            style={{ "--node-left": `${columnX(column)}px` } as CSSProperties}
            variant="overline"
          >
            {title}
          </AppTypography>
        ))}
        <svg
          aria-hidden="true"
          className="storage-pools__edges"
          viewBox={`0 0 ${width} ${HEADER + graph.height}`}
        >
          <Edges
            columnWidth={columnWidth}
            columnX={columnX}
            edges={graph.edges}
            highlight={highlight}
            nodes={graph.nodes}
            paused={paused}
          />
        </svg>
        {graph.nodes.map((node) => (
          <div
            className={`storage-pools__node${node.kind === "drive" ? " storage-pools__card storage-pools__disks" : ""}`}
            data-kind={node.kind}
            key={node.id}
            style={
              {
                "--node-left": `${columnX(node.column)}px`,
                "--node-top": `${HEADER + node.top}px`,
                "--node-width": `${columnWidth}px`,
                "--node-height": `${node.height}px`,
              } as CSSProperties
            }
          >
            {card(node)}
          </div>
        ))}
      </div>
      {selected && (
        <>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              marginBottom: 8,
            }}
          >
            <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
              <AppTypography style={{ fontWeight: 600 }} variant="h6">
                Details · {selected.pool.name}
              </AppTypography>
              <AppActionIconButton
                icon="mdi:pencil"
                label="Rename pool"
                onClick={() =>
                  setDialog({
                    kind: "rename",
                    uuid: selected.fs.uuid,
                    label: selected.fs.label,
                  })
                }
              />
            </div>
            <AppButton
              color="error"
              onClick={() =>
                setDialog({
                  kind: "destroy",
                  uuid: selected.fs.uuid,
                  name: selected.pool.name,
                  disks: selected.fs.devices
                    .map(({ path }) => path)
                    .filter(Boolean),
                })
              }
              size="small"
              variant="outlined"
            >
              Destroy pool
            </AppButton>
          </div>
          <AppGrid
            container
            spacing={DASHBOARD_CARD_SPACING}
            style={{ marginBottom: 16 }}
          >
            <AppGrid size={12}>
              <BtrfsHealthBanner
                filesystem={selected.fs}
                key={selected.fs.uuid}
              />
            </AppGrid>
            <AppGrid size={{ xs: 12, md: 7 }}>
              <BtrfsDevicesCard
                filesystem={selected.fs}
                key={selected.fs.uuid}
              />
            </AppGrid>
            <AppGrid size={{ xs: 12, md: 5 }}>
              <BtrfsSpaceCard filesystem={selected.fs} />
            </AppGrid>
            <AppGrid size={12}>
              <BtrfsSettingsCard
                key={selected.fs.uuid}
                mountpoint={selected.fs.mountpoints[0]?.mountpoint}
                uuid={selected.fs.uuid}
              />
            </AppGrid>
          </AppGrid>
        </>
      )}
      {selectedArray && (
        <MdRaidDetails array={selectedArray} key={selectedArray.name} />
      )}
      {dialog?.kind === "create" && (
        <BtrfsCreatePoolDialog
          isPending={createPool.isPending}
          onClose={closeDialog}
          onConfirm={(request) =>
            createPool.mutate(request, { onSuccess: closeDialog })
          }
          progress={createStep}
          pooledDevices={btrfs.flatMap((fs) =>
            fs.devices.map(({ path }) => path),
          )}
        />
      )}
      {dialog?.kind === "rename" && (
        <GeneralDialog fullWidth maxWidth="sm" onClose={closeDialog} open>
          <AppDialogTitle>Rename pool</AppDialogTitle>
          <AppDialogContent>
            <AppTextField
              autoFocus
              fullWidth
              label="Name"
              onChange={(event) =>
                setDialog({ ...dialog, label: event.target.value })
              }
              value={dialog.label}
            />
          </AppDialogContent>
          <AppDialogActions>
            <AppButton onClick={closeDialog}>Cancel</AppButton>
            <AppButton
              disabled={isRenaming || dialog.label.trim() === ""}
              onClick={() =>
                renamePool(
                  { uuid: dialog.uuid, label: dialog.label.trim() },
                  { onSuccess: closeDialog },
                )
              }
              variant="contained"
            >
              Rename
            </AppButton>
          </AppDialogActions>
        </GeneralDialog>
      )}
      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={closeDialog}
        open={dialog?.kind === "destroy"}
      >
        <AppDialogTitle>Destroy pool</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            {dialog?.kind === "destroy" &&
              `Destroy ${dialog.name}? It is unmounted, removed from /etc/fstab and the btrfs signature is erased from ${dialog.disks.join(", ")}. Everything on it is lost.`}
          </AppDialogContentText>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={closeDialog}>Cancel</AppButton>
          <AppButton
            color="error"
            disabled={isDestroying}
            onClick={() => {
              if (dialog?.kind !== "destroy") return;
              destroyPool(
                { uuid: dialog.uuid },
                {
                  onSuccess: () => {
                    closeDialog();
                    onSelectPool(undefined);
                  },
                },
              );
            }}
            variant="contained"
          >
            Destroy
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </>
  );
};

export default StoragePools;
