import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

import { linuxio } from "@/api";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useCapability } from "@/hooks/useCapabilities";
import { DASHBOARD_CARD_SPACING } from "@/theme/constants";
import { formatFileSize } from "@/utils/formaters";

import BtrfsDevicesCard from "../../btrfs/BtrfsDevicesCard";
import BtrfsSpaceCard from "../../btrfs/BtrfsSpaceCard";
import { describePool } from "../storagePools";
import type { DriveInfo } from "../types";
import {
  findBackingDrive,
  getTemperature,
  getTemperatureColor,
} from "../utils";
import "./storage-pools.css";

/** A line from `count` evenly spaced exits on the right to one entry on the left. */
const Connector = ({ count = 1 }: { count?: number }) => {
  const ys = Array.from({ length: count }, (_, i) => ((i + 0.5) / count) * 100);
  const d =
    `M0 50H50M50 ${ys[0]}V${ys[ys.length - 1]}` +
    ys.map((y) => `M50 ${y}H100`).join("");
  return (
    <div aria-hidden="true" className="storage-pools__link">
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
  const content: ReactNode = (
    <>
      <Icon
        className="storage-pools__icon"
        height={26}
        icon="mdi:harddisk"
        width={26}
      />
      <span>
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

interface StoragePoolsProps {
  drives: DriveInfo[];
  onInspectDrive: (name: string) => void;
  onOpenVolume: (mountpoint: string) => void;
}

/**
 * Every drive, with btrfs filesystems drawn as disks -> pool -> volumes and
 * the selected pool's disk and RAID management below. Pools need btrfs and
 * privilege; the drives themselves are always listed.
 */
const StoragePools = ({
  drives,
  onInspectDrive,
  onOpenVolume,
}: StoragePoolsProps) => {
  const { privileged } = useAuth();
  const { isEnabled } = useCapability("btrfsAvailable");
  const poolsEnabled = privileged && isEnabled;
  const { data: filesystems = [], isPending } = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: poolsEnabled,
    refetchInterval: 10000,
  });
  const [selectedUuid, setSelectedUuid] = useState<string>();

  const pools = filesystems.map((fs) => ({ fs, pool: describePool(fs) }));
  const pooled = new Set(
    pools.flatMap(({ pool }) =>
      pool.disks.flatMap(
        (disk) => findBackingDrive(disk.name, drives)?.name ?? [],
      ),
    ),
  );
  const others = drives.filter((drive) => !pooled.has(drive.name));
  const selected =
    filesystems.find((fs) => fs.uuid === selectedUuid) ?? filesystems[0];

  // Until the pools load every drive would look pool-less, then jump into
  // its pool; draw nothing rather than that flicker.
  if (poolsEnabled && isPending) return null;

  if (drives.length === 0 && pools.length === 0) {
    return (
      <AppTypography color="text.secondary" style={{ marginBottom: 16 }}>
        No drives found.
      </AppTypography>
    );
  }

  return (
    <>
      <AppTypography style={{ marginBottom: 8, fontWeight: 600 }} variant="h6">
        Storage space
      </AppTypography>
      {pools.length > 0 && (
        <div className="storage-pools">
          {pools.map(({ pool }) => (
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
                aria-label={`Pool ${pool.name}`}
                aria-pressed={pool.uuid === selected.uuid}
                className="storage-pools__card storage-pools__pool"
                color="inherit"
                onClick={() => setSelectedUuid(pool.uuid)}
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
              <Connector count={Math.max(pool.volumes.length, 1)} />
              <div className="storage-pools__volumes">
                {pool.volumes.map((mountpoint) => (
                  <div className="storage-pools__slot" key={mountpoint}>
                    <AppButton
                      aria-label={`Volume ${mountpoint}`}
                      className="storage-pools__card storage-pools__volume"
                      color="inherit"
                      onClick={() => onOpenVolume(mountpoint)}
                    >
                      <Icon
                        className="storage-pools__icon"
                        height={26}
                        icon="mdi:chart-pie"
                        width={26}
                      />
                      <span>
                        <AppTypography fontWeight={600} noWrap variant="body2">
                          {mountpoint}
                        </AppTypography>
                      </span>
                      <AppChip
                        color="primary"
                        label="Btrfs"
                        size="small"
                        variant="soft"
                      />
                      <AppTypography color="text.secondary" variant="caption">
                        Free {formatFileSize(pool.freeBytes)}
                      </AppTypography>
                    </AppButton>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
      {others.length > 0 && (
        <>
          {pools.length > 0 && (
            <AppTypography
              color="text.secondary"
              style={{ marginBottom: 8 }}
              variant="subtitle2"
            >
              Other disks
            </AppTypography>
          )}
          <div className="storage-pools__others">
            {others.map((drive) => (
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
            Pool details · {describePool(selected).name}
          </AppTypography>
          <AppGrid
            container
            spacing={DASHBOARD_CARD_SPACING}
            style={{ marginBottom: 16 }}
          >
            <AppGrid size={{ xs: 12, md: 7 }}>
              <BtrfsDevicesCard filesystem={selected} key={selected.uuid} />
            </AppGrid>
            <AppGrid size={{ xs: 12, md: 5 }}>
              <BtrfsSpaceCard filesystem={selected} />
            </AppGrid>
          </AppGrid>
        </>
      )}
    </>
  );
};

export default StoragePools;
