import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import {
  linuxio,
  useCallMutation,
  type BtrfsFilesystem,
  type BtrfsOpProgress,
  type FilesystemInfo,
  type LiveDiskRates,
  type MDArray,
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
import { DASHBOARD_CARD_SPACING } from "@/theme/constants";
import { formatFileSize, formatThroughput } from "@/utils/formatters";

import { driveIcon } from "./DriveCard";
import MdRaidDetails from "./MdRaidDetails";
import {
  buildStorageLayout,
  deviceTypeLabel,
  fsTypeLabel,
} from "./storagePools";
import type { DriveInfo } from "./types";
import {
  findBackingDrive,
  getHealthColor,
  getTemperature,
  getTemperatureColor,
} from "./utils";
import BtrfsCreatePoolDialog from "../btrfs/BtrfsCreatePoolDialog";
import BtrfsDevicesCard from "../btrfs/BtrfsDevicesCard";
import BtrfsHealthBanner from "../btrfs/BtrfsHealthBanner";
import BtrfsSettingsCard from "../btrfs/BtrfsSettingsCard";
import BtrfsSpaceCard from "../btrfs/BtrfsSpaceCard";
import "./storage-pools.css";

/** Percent heights of the centres of `count` equal slots. */
const slotCentres = (count: number) =>
  Array.from({ length: count }, (_, i) => ((i + 0.5) / count) * 100);

/**
 * Lines from `from` evenly spaced entries on the left to `to` exits on the
 * right; an entry with `rates` carries its disk's reads forward and writes
 * back, except into the last `idle` exits (partitions nothing uses).
 */
const Connector = ({
  from = 1,
  to = 1,
  idle = 0,
  span = false,
  rates = [],
  paused = false,
}: {
  from?: number;
  to?: number;
  idle?: number;
  span?: boolean;
  rates?: (LiveDiskRates | undefined)[];
  paused?: boolean;
}) => {
  const entries = slotCentres(from);
  const ys = slotCentres(to);
  return (
    <div
      aria-hidden="true"
      className={`storage-pools__link${span ? " storage-pools__link--span" : ""}`}
    >
      <svg preserveAspectRatio="none" viewBox="0 0 100 100">
        {entries.flatMap((entry, index) =>
          ys.map((y, exit) => {
            const flow = exit < to - idle ? rates[index] : undefined;
            return (
              <AppTopologyEdge
                className="storage-pools__line"
                d={`M0 ${entry}H50V${y}H100`}
                forward={flow?.read_bytes_per_sec}
                highlighted={false}
                key={`${entry}:${y}`}
                paused={paused}
                reverse={flow?.write_bytes_per_sec}
              />
            );
          }),
        )}
      </svg>
      {entries.map((y) => (
        <span className="storage-pools__dot" key={y} style={{ top: `${y}%` }} />
      ))}
      {ys.map((y) => (
        <span
          className="storage-pools__arrow"
          key={y}
          style={{ top: `${y}%` }}
        />
      ))}
    </div>
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

interface DiskRowProps {
  caption?: string;
  /** The physical drive behind this disk; without one the row is not a link. */
  drive: DriveInfo | null;
  missing?: boolean;
  name: string;
  onInspectDrive: (name: string) => void;
  sizeBytes: number;
}

/** One disk: name, size and temperature, linking to the drive's details. */
const DiskRow = ({
  name,
  caption,
  sizeBytes,
  missing,
  drive,
  onInspectDrive,
}: DiskRowProps) => {
  const temperature = getTemperature(drive?.smart);
  const health = HEALTH[getHealthColor(drive?.smart)];
  const content: ReactNode = (
    <>
      <Icon
        className="storage-pools__icon"
        height={24}
        icon={driveIcon(drive?.transport, drive?.rotational)}
        width={24}
      />
      {drive && (
        <StatusDot
          color={health.color}
          size={8}
          style={{ marginLeft: -6 }}
          tooltip={health.label}
        />
      )}
      <span className="storage-pools__name">
        <AppTypography noWrap variant="body2">
          {name}
        </AppTypography>
        {caption && (
          <AppTypography color="text.secondary" noWrap variant="caption">
            {caption}
          </AppTypography>
        )}
        {drive?.rates && (
          <AppTypography color="text.secondary" noWrap variant="caption">
            Read {formatThroughput(drive.rates.read_bytes_per_sec)} · Write{" "}
            {formatThroughput(drive.rates.write_bytes_per_sec)}
          </AppTypography>
        )}
      </span>
      {missing ? (
        <AppChip color="error" label="Missing" size="small" variant="soft" />
      ) : (
        <AppTypography color="text.secondary" variant="caption">
          {formatFileSize(sizeBytes)}
        </AppTypography>
      )}
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
    </>
  );
  if (!drive) return <div className="storage-pools__disk">{content}</div>;
  return (
    <AppButton
      aria-label={`Drive details ${drive.name}`}
      className="storage-pools__disk storage-pools__disk--link"
      color="inherit"
      onClick={() => onInspectDrive(drive.name)}
    >
      {content}
    </AppButton>
  );
};

/** A disk in a column of its own card, so each gets its own connector line. */
const DiskCard = (props: DiskRowProps) => (
  <div className="storage-pools__slot">
    <div className="storage-pools__card">
      <DiskRow {...props} />
    </div>
  </div>
);

/**
 * A mounted filesystem; opens its filesystem card. On a plain disk it names
 * its partition, which a pool or source card already stands for elsewhere.
 */
const VolumeCard = ({
  volume,
  onOpenVolume,
  showDevice,
}: {
  volume: FilesystemInfo;
  onOpenVolume: (mountpoint: string) => void;
  showDevice: boolean;
}) => (
  <div className="storage-pools__slot">
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
        {showDevice && (
          <AppTypography color="text.secondary" noWrap variant="caption">
            {volume.device.replace(/^\/dev\//, "")}
          </AppTypography>
        )}
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
  </div>
);

/** A partition nothing mounts or builds on. */
const PartitionCard = ({ partition }: { partition: StorageBlockDevice }) => (
  <div className="storage-pools__slot">
    <div className="storage-pools__card storage-pools__volume">
      <Icon
        className="storage-pools__icon"
        height={26}
        icon="mdi:chart-pie-outline"
        width={26}
      />
      <span>
        <AppTypography fontWeight={600} noWrap variant="body2">
          {partition.name}
        </AppTypography>
        <AppTypography color="text.secondary" noWrap variant="caption">
          Not mounted · {formatFileSize(partition.sizeBytes)}
        </AppTypography>
      </span>
      {partition.fsType ? (
        <AppChip
          label={fsTypeLabel(partition.fsType)}
          size="small"
          variant="soft"
        />
      ) : (
        <AppTypography color="text.secondary" variant="caption">
          No filesystem
        </AppTypography>
      )}
    </div>
  </div>
);

const Volumes = ({
  volumes,
  partitions = [],
  onOpenVolume,
  showDevice = false,
}: {
  volumes: FilesystemInfo[];
  partitions?: StorageBlockDevice[];
  onOpenVolume: (mountpoint: string) => void;
  showDevice?: boolean;
}) => (
  <div className="storage-pools__column">
    {volumes.map((volume) => (
      <VolumeCard
        key={volume.mountpoint}
        onOpenVolume={onOpenVolume}
        showDevice={showDevice}
        volume={volume}
      />
    ))}
    {partitions.map((partition) => (
      <PartitionCard key={partition.path} partition={partition} />
    ))}
  </div>
);

/**
 * A RAID, LVM or mapper device over its drives; an md array opens its
 * details, the others are not managed here.
 */
const SourceCard = ({
  array,
  label,
  onSelectRaid,
  selectedRaid,
  sizeBytes,
  type,
}: {
  array?: MDArray;
  label: string;
  onSelectRaid: (name?: string) => void;
  selectedRaid?: string;
  sizeBytes: number;
  type: string;
}) => {
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
      <AppChip
        color="primary"
        label={deviceTypeLabel(type)}
        size="small"
        variant="soft"
      />
      {array && array.degraded > 0 && (
        <AppChip color="error" label="Degraded" size="small" variant="soft" />
      )}
      <AppTypography color="text.secondary" variant="caption">
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
 * Every drive and every mounted filesystem, drawn as disks -> pool -> volumes
 * for btrfs pools and RAID, LVM or mapper devices, disk -> volumes and unused
 * partitions for plain partitions, and source -> volumes for network shares
 * and devices with no drive found under them. Lines out of a disk carry its
 * live reads and writes. The selected pool's health, disk and RAID
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
  const closeDialog = () => setDialog(null);
  // A task, not a call: mkfs trims every disk first, which can outlast any
  // request timeout on large SSDs.
  const [createStep, setCreateStep] = useState<string | null>(null);
  const createPool = linuxio.storage.btrfs_pool_create.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >({
    onProgress: (progress) => setCreateStep(progress.detail?.message ?? null),
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

  const {
    poolStacks,
    driveStacks,
    sourceStacks,
    idleDrives: unpooled,
  } = buildStorageLayout(
    btrfs,
    filesystems,
    drives,
    nfsMounts,
    topology?.devices ?? [],
    offline.flatMap((pool) => pool.devices),
  );
  // Disks of an unmounted pool are drawn under it, not as idle.
  const offlineDrives = new Set(
    offline.flatMap((pool) =>
      pool.devices.map(
        (path) => findBackingDrive(path.replace(/^\/dev\//, ""), drives)?.name,
      ),
    ),
  );
  const idleDrives = unpooled.filter((drive) => !offlineDrives.has(drive.name));
  const selected = poolStacks.find(({ fs }) => fs.uuid === selectedPool);
  const { data: arrays = [] } = useQuery({
    ...linuxio.storage.md_list,
    enabled: sourceStacks.some(({ type }) => type.startsWith("raid")),
    refetchInterval: 5000,
  });
  const selectedArray = arrays.find(({ name }) => name === selectedRaid);

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
          {drives.some((drive) => drive.rates) && (
            <AppButton
              aria-pressed={paused}
              color="inherit"
              onClick={() => setPaused(!paused)}
              size="small"
              variant="outlined"
            >
              {paused ? "Resume animation" : "Pause animation"}
            </AppButton>
          )}
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
      <div className="storage-pools">
        {driveStacks.map(({ drive, partitions, volumes }) => (
          <div className="storage-pools__stack" key={drive.name}>
            <div className="storage-pools__card storage-pools__disks">
              <DiskRow
                caption={drive.model}
                drive={drive}
                name={drive.name}
                onInspectDrive={onInspectDrive}
                sizeBytes={drive.sizeBytes}
              />
            </div>
            <Connector
              idle={partitions.length}
              paused={paused}
              rates={[drive.rates]}
              span
              to={volumes.length + partitions.length}
            />
            <Volumes
              onOpenVolume={onOpenVolume}
              partitions={partitions}
              showDevice
              volumes={volumes}
            />
          </div>
        ))}
        {poolStacks.map(({ pool, volumes }) => (
          <div className="storage-pools__stack" key={pool.uuid}>
            <div className="storage-pools__column">
              {pool.disks.map((disk) => (
                <DiskCard
                  drive={findBackingDrive(disk.name, drives)}
                  key={disk.key}
                  missing={disk.missing}
                  name={disk.name}
                  onInspectDrive={onInspectDrive}
                  sizeBytes={disk.sizeBytes}
                />
              ))}
            </div>
            <Connector
              from={pool.disks.length}
              paused={paused}
              rates={pool.disks.map(
                (disk) => findBackingDrive(disk.name, drives)?.rates,
              )}
            />
            <AppButton
              aria-label={pool.name}
              aria-pressed={pool.uuid === selected?.pool.uuid}
              className="storage-pools__card storage-pools__pool"
              color="inherit"
              onClick={() =>
                onSelectPool(pool.uuid === selectedPool ? undefined : pool.uuid)
              }
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
                Allocated {formatFileSize(pool.allocatedBytes)}
              </AppTypography>
              <AppTypography color="text.secondary" variant="caption">
                Free {formatFileSize(pool.freeBytes)}
              </AppTypography>
            </AppButton>
            {volumes.length > 0 && (
              <>
                <Connector to={volumes.length} />
                <Volumes onOpenVolume={onOpenVolume} volumes={volumes} />
              </>
            )}
          </div>
        ))}
        {offline.map((pool) => {
          const missing = pool.numDevices - pool.devices.length;
          const name = pool.label || pool.uuid;
          return (
            <div className="storage-pools__stack" key={pool.uuid}>
              <div className="storage-pools__column">
                {pool.devices.map((path) => {
                  const diskName = path.replace(/^\/dev\//, "");
                  const drive = findBackingDrive(diskName, drives);
                  return (
                    <DiskCard
                      drive={drive}
                      key={path}
                      name={diskName}
                      onInspectDrive={onInspectDrive}
                      sizeBytes={drive?.sizeBytes ?? 0}
                    />
                  );
                })}
                {missing > 0 && (
                  <DiskCard
                    drive={null}
                    missing
                    name={`${missing} missing disk${missing > 1 ? "s" : ""}`}
                    onInspectDrive={onInspectDrive}
                    sizeBytes={0}
                  />
                )}
              </div>
              <Connector from={pool.devices.length + (missing > 0 ? 1 : 0)} />
              <div className="storage-pools__card storage-pools__pool">
                <Icon
                  className="storage-pools__icon"
                  height={36}
                  icon="mdi:database-off-outline"
                  width={36}
                />
                <AppTypography fontWeight={600} noWrap variant="body2">
                  {name}
                </AppTypography>
                <AppChip
                  color={missing > 0 ? "error" : "warning"}
                  label={missing > 0 ? "Degraded" : "Not mounted"}
                  size="small"
                  variant="soft"
                />
                <AppTypography color="text.secondary" variant="caption">
                  {pool.mountpoint
                    ? `fstab: ${pool.mountpoint}`
                    : "Not in /etc/fstab"}
                </AppTypography>
                <div style={{ display: "flex", gap: 4 }}>
                  <AppButton
                    disabled={isMounting}
                    onClick={() =>
                      mountPool({ uuid: pool.uuid, degraded: missing > 0 })
                    }
                    size="small"
                    variant="outlined"
                  >
                    {missing > 0 ? "Mount degraded" : "Mount"}
                  </AppButton>
                  <AppButton
                    color="error"
                    onClick={() =>
                      setDialog({
                        kind: "destroy",
                        uuid: pool.uuid,
                        name,
                        disks: pool.devices,
                      })
                    }
                    size="small"
                  >
                    Destroy
                  </AppButton>
                </div>
              </div>
            </div>
          );
        })}
        {sourceStacks.map(
          ({
            drives: members,
            key,
            label,
            network,
            sizeBytes,
            type,
            volumes,
          }) =>
            members.length > 0 ? (
              <div className="storage-pools__stack" key={key}>
                <div className="storage-pools__column">
                  {members.map((drive) => (
                    <DiskCard
                      caption={drive.model}
                      drive={drive}
                      key={drive.name}
                      name={drive.name}
                      onInspectDrive={onInspectDrive}
                      sizeBytes={drive.sizeBytes}
                    />
                  ))}
                </div>
                <Connector
                  from={members.length}
                  paused={paused}
                  rates={members.map((drive) => drive.rates)}
                />
                <SourceCard
                  array={arrays.find(({ path }) => path === label)}
                  label={label}
                  onSelectRaid={onSelectRaid}
                  selectedRaid={selectedRaid}
                  sizeBytes={sizeBytes}
                  type={type}
                />
                <Connector to={volumes.length} />
                <Volumes onOpenVolume={onOpenVolume} volumes={volumes} />
              </div>
            ) : (
              <div className="storage-pools__stack" key={key}>
                <div className="storage-pools__card storage-pools__disks">
                  <div className="storage-pools__disk">
                    <Icon
                      className="storage-pools__icon"
                      height={24}
                      icon={
                        network ? "mdi:server-network" : "mdi:layers-outline"
                      }
                      width={24}
                    />
                    <span className="storage-pools__name">
                      <AppTypography noWrap variant="body2">
                        {label}
                      </AppTypography>
                      <AppTypography color="text.secondary" variant="caption">
                        {network ? "Network share" : "No physical disk found"}
                      </AppTypography>
                    </span>
                  </div>
                </div>
                <Connector span to={volumes.length} />
                <Volumes onOpenVolume={onOpenVolume} volumes={volumes} />
              </div>
            ),
        )}
      </div>
      {idleDrives.length > 0 && (
        <>
          <AppTypography
            color="text.secondary"
            style={{ marginBottom: 8 }}
            variant="subtitle2"
          >
            Disks without volumes
          </AppTypography>
          <div className="storage-pools__others">
            {idleDrives.map((drive) => (
              <div className="storage-pools__card" key={drive.name}>
                <DiskRow
                  caption={drive.model}
                  drive={drive}
                  name={drive.name}
                  onInspectDrive={onInspectDrive}
                  sizeBytes={drive.sizeBytes}
                />
              </div>
            ))}
          </div>
        </>
      )}
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
