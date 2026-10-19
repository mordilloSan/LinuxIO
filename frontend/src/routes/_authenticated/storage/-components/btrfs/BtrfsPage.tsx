import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio, useCallMutation, type BtrfsFilesystem } from "@/api";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppGrid from "@/components/ui/AppGrid";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";

import BtrfsDevicesCard from "./BtrfsDevicesCard";
import BtrfsHealthCard from "./BtrfsHealthCard";
import BtrfsReplicationCard from "./BtrfsReplicationCard";
import BtrfsSettingsCard from "./BtrfsSettingsCard";
import { BTRFS_TOAST_META } from "./btrfsShared";
import BtrfsSnapshotRulesCard from "./BtrfsSnapshotRulesCard";
import BtrfsSnapshotsCard from "./BtrfsSnapshotsCard";
import BtrfsSpaceCard from "./BtrfsSpaceCard";
import BtrfsSubvolumesCard from "./BtrfsSubvolumesCard";

interface BtrfsPageProps {
  selectedUuid?: string;
  onSelect: (uuid: string) => void;
}

const filesystemLabel = (fs: BtrfsFilesystem) =>
  `${fs.label || fs.uuid} · ${fs.mountpoints[0]?.mountpoint ?? ""}`;

const BtrfsFilesystems = ({ selectedUuid, onSelect }: BtrfsPageProps) => {
  const { data: filesystems } = useSuspenseQuery({
    ...linuxio.storage.btrfs_list,
    refetchInterval: 10000,
  });

  const { mutate: createSnapshot } = useCallMutation(
    linuxio.storage.btrfs_snapshot_create,
    {
      success: "Snapshot created",
      error: "Failed to create snapshot",
      toast: BTRFS_TOAST_META,
    },
  );

  if (filesystems.length === 0) {
    return (
      <AppAlert severity="info">No btrfs filesystems are mounted.</AppAlert>
    );
  }

  const selected =
    filesystems.find((fs) => fs.uuid === selectedUuid) ?? filesystems[0];

  return (
    <>
      <AppSelect
        label="Filesystem"
        onChange={(event) => onSelect(event.target.value)}
        value={selected.uuid}
      >
        {filesystems.map((fs) => (
          <option key={fs.uuid} value={fs.uuid}>
            {filesystemLabel(fs)}
          </option>
        ))}
      </AppSelect>
      <AppGrid container spacing={2}>
        <AppGrid size={{ xs: 12, md: 6, lg: 4 }}>
          <BtrfsHealthCard filesystem={selected} />
        </AppGrid>
        <AppGrid size={{ xs: 12, md: 6, lg: 4 }}>
          <BtrfsDevicesCard filesystem={selected} key={selected.uuid} />
        </AppGrid>
        <AppGrid size={{ xs: 12, md: 6, lg: 4 }}>
          <BtrfsSpaceCard filesystem={selected} />
        </AppGrid>
        <AppGrid size={{ xs: 12 }}>
          <BtrfsSubvolumesCard
            onSnapshot={(subvolume) =>
              createSnapshot({ uuid: selected.uuid, path: subvolume.path })
            }
            uuid={selected.uuid}
          />
        </AppGrid>
        <AppGrid size={{ xs: 12 }}>
          <BtrfsSnapshotsCard uuid={selected.uuid} />
        </AppGrid>
        <AppGrid size={{ xs: 12 }}>
          <BtrfsSnapshotRulesCard uuid={selected.uuid} />
        </AppGrid>
        <AppGrid size={{ xs: 12 }}>
          <BtrfsReplicationCard key={selected.uuid} uuid={selected.uuid} />
        </AppGrid>
        <AppGrid size={{ xs: 12 }}>
          <BtrfsSettingsCard
            key={selected.uuid}
            mountpoint={selected.mountpoints[0]?.mountpoint}
            uuid={selected.uuid}
          />
        </AppGrid>
      </AppGrid>
    </>
  );
};

const BtrfsPage = (props: BtrfsPageProps) => {
  const { status, reason } = useCapability("btrfsAvailable");

  if (status !== "available") {
    return (
      <div style={{ padding: "var(--app-space-12)" }}>
        <AppAlert severity={status === "unknown" ? "info" : "warning"}>
          <AppAlertTitle>
            {status === "unknown" ? "Checking btrfs" : "btrfs unavailable"}
          </AppAlertTitle>
          <AppTypography variant="body2">{reason}</AppTypography>
        </AppAlert>
      </div>
    );
  }

  return <BtrfsFilesystems {...props} />;
};

export default BtrfsPage;
