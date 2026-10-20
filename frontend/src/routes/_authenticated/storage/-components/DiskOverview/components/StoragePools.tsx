import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { linuxio, type FilesystemInfo, type NFSMount } from "@/api";
import { driveIcon } from "@/components/cards/DriveCard";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import useAuth from "@/hooks/useAuth";
import { useCapability } from "@/hooks/useCapabilities";
import { DASHBOARD_CARD_SPACING } from "@/theme/constants";
import { formatFileSize } from "@/utils/formaters";

import BtrfsDevicesCard from "../../btrfs/BtrfsDevicesCard";
import BtrfsSpaceCard from "../../btrfs/BtrfsSpaceCard";
import { buildStorageLayout, fsTypeLabel } from "../storagePools";
import type { DriveInfo } from "../types";
import {
  findBackingDrive,
  getHealthColor,
  getTemperature,
  getTemperatureColor,
} from "../utils";
import "./storage-pools.css";

/** A line from `count` evenly spaced exits on the right to one entry on the left. */
const Connector = ({
  count = 1,
  span = false,
}: {
  count?: number;
  span?: boolean;
}) => {
  const ys = Array.from({ length: count }, (_, i) => ((i + 0.5) / count) * 100);
  const d =
    `M0 50H50M50 ${ys[0]}V${ys[ys.length - 1]}` +
    ys.map((y) => `M50 ${y}H100`).join("");
  return (
    <div
      aria-hidden="true"
      className={`storage-pools__link${span ? " storage-pools__link--span" : ""}`}
    >
      <svg preserveAspectRatio="none" viewBox="0 0 100 100">
        <path d={d} />
      </svg>
      <span className="storage-pools__dot" />
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

/** A mounted filesystem; opens its filesystem card. */
const VolumeCard = ({
  volume,
  onOpenVolume,
}: {
  volume: FilesystemInfo;
  onOpenVolume: (mountpoint: string) => void;
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

const Volumes = ({
  volumes,
  onOpenVolume,
}: {
  volumes: FilesystemInfo[];
  onOpenVolume: (mountpoint: string) => void;
}) => (
  <div className="storage-pools__volumes">
    {volumes.map((volume) => (
      <VolumeCard
        key={volume.mountpoint}
        onOpenVolume={onOpenVolume}
        volume={volume}
      />
    ))}
  </div>
);

interface StoragePoolsProps {
  drives: DriveInfo[];
  filesystems: FilesystemInfo[];
  nfsMounts: NFSMount[];
  onInspectDrive: (name: string) => void;
  onOpenVolume: (mountpoint: string) => void;
}

/**
 * Every drive and every mounted filesystem, drawn as disks -> pool -> volumes
 * for btrfs pools, disk -> volumes for plain partitions and source -> volumes
 * for network shares and mapper devices. The selected pool's disk and RAID
 * management sits below. Pools need btrfs and privilege; the rest does not.
 */
const StoragePools = ({
  drives,
  filesystems,
  nfsMounts,
  onInspectDrive,
  onOpenVolume,
}: StoragePoolsProps) => {
  const { privileged } = useAuth();
  const { isEnabled } = useCapability("btrfsAvailable");
  const poolsEnabled = privileged && isEnabled;
  const { data: btrfs = [], isPending } = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: poolsEnabled,
    refetchInterval: 10000,
  });
  const [selectedUuid, setSelectedUuid] = useState<string>();

  const { poolStacks, driveStacks, sourceStacks, idleDrives } =
    buildStorageLayout(btrfs, filesystems, drives, nfsMounts);
  const selected = poolStacks.find(({ fs }) => fs.uuid === selectedUuid);

  // Until the pools load, btrfs volumes would sit under their drives and then
  // jump into their pool; draw nothing rather than that flicker.
  if (poolsEnabled && isPending) return null;

  if (drives.length === 0 && filesystems.length === 0 && btrfs.length === 0) {
    return (
      <AppTypography color="text.secondary" style={{ marginBottom: 16 }}>
        No drives or volumes found.
      </AppTypography>
    );
  }

  return (
    <>
      <AppTypography style={{ marginBottom: 8, fontWeight: 600 }} variant="h6">
        Storage space
      </AppTypography>
      <div className="storage-pools">
        {poolStacks.map(({ pool, volumes }) => (
          <div className="storage-pools__stack" key={pool.uuid}>
            <div className="storage-pools__card storage-pools__disks">
              {pool.disks.map((disk) => (
                <DiskRow
                  drive={findBackingDrive(disk.name, drives)}
                  key={disk.key}
                  missing={disk.missing}
                  name={disk.name}
                  onInspectDrive={onInspectDrive}
                  sizeBytes={disk.sizeBytes}
                />
              ))}
            </div>
            <Connector />
            <AppButton
              aria-label={pool.name}
              aria-pressed={pool.uuid === selected?.pool.uuid}
              className="storage-pools__card storage-pools__pool"
              color="inherit"
              onClick={() =>
                setSelectedUuid(
                  pool.uuid === selectedUuid ? undefined : pool.uuid,
                )
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
                <Connector count={volumes.length} />
                <Volumes onOpenVolume={onOpenVolume} volumes={volumes} />
              </>
            )}
          </div>
        ))}
        {driveStacks.map(({ drive, volumes }) => (
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
            <Connector count={volumes.length} span />
            <Volumes onOpenVolume={onOpenVolume} volumes={volumes} />
          </div>
        ))}
        {sourceStacks.map(({ key, label, network, volumes }) => (
          <div className="storage-pools__stack" key={key}>
            <div className="storage-pools__card storage-pools__disks">
              <div className="storage-pools__disk">
                <Icon
                  className="storage-pools__icon"
                  height={24}
                  icon={network ? "mdi:server-network" : "mdi:layers-outline"}
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
            <Connector count={volumes.length} span />
            <Volumes onOpenVolume={onOpenVolume} volumes={volumes} />
          </div>
        ))}
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
          <AppTypography
            style={{ marginBottom: 8, fontWeight: 600 }}
            variant="h6"
          >
            Details · {selected.pool.name}
          </AppTypography>
          <AppGrid
            container
            spacing={DASHBOARD_CARD_SPACING}
            style={{ marginBottom: 16 }}
          >
            <AppGrid size={{ xs: 12, md: 7 }}>
              <BtrfsDevicesCard
                filesystem={selected.fs}
                key={selected.fs.uuid}
              />
            </AppGrid>
            <AppGrid size={{ xs: 12, md: 5 }}>
              <BtrfsSpaceCard filesystem={selected.fs} />
            </AppGrid>
          </AppGrid>
        </>
      )}
    </>
  );
};

export default StoragePools;
