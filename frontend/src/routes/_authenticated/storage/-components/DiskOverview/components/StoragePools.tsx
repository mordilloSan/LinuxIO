import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

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

interface StoragePoolsProps {
  onOpenVolume: (mountpoint: string) => void;
}

/**
 * Disks -> pool -> volumes for every mounted btrfs filesystem, with the pool's
 * disk and RAID management below. Hidden without btrfs or without privilege.
 */
const StoragePools = ({ onOpenVolume }: StoragePoolsProps) => {
  const { privileged } = useAuth();
  const { isEnabled } = useCapability("btrfsAvailable");
  const { data: filesystems = [] } = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: privileged && isEnabled,
    refetchInterval: 10000,
  });
  const [selectedUuid, setSelectedUuid] = useState<string>();

  if (filesystems.length === 0) return null;

  const selected =
    filesystems.find((fs) => fs.uuid === selectedUuid) ?? filesystems[0];

  return (
    <>
      <AppTypography style={{ marginBottom: 8, fontWeight: 600 }} variant="h6">
        Storage space
      </AppTypography>
      <div className="storage-pools">
        {filesystems.map((fs) => {
          const pool = describePool(fs);
          return (
            <div className="storage-pools__stack" key={pool.uuid}>
              <div className="storage-pools__card storage-pools__disks">
                {pool.disks.map((disk) => (
                  <div className="storage-pools__disk" key={disk.key}>
                    <Icon
                      className="storage-pools__icon"
                      height={26}
                      icon="mdi:harddisk"
                      width={26}
                    />
                    <AppTypography noWrap variant="body2">
                      {disk.name}
                    </AppTypography>
                    {disk.missing ? (
                      <AppChip
                        color="error"
                        label="Missing"
                        size="small"
                        variant="soft"
                      />
                    ) : (
                      <AppTypography color="text.secondary" variant="caption">
                        {formatFileSize(disk.sizeBytes)}
                      </AppTypography>
                    )}
                  </div>
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
          );
        })}
      </div>
      <AppTypography style={{ marginBottom: 8, fontWeight: 600 }} variant="h6">
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
  );
};

export default StoragePools;
