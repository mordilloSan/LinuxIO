import { useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio, type BtrfsFilesystem } from "@/api";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppCollapse from "@/components/ui/AppCollapse";
import AppGrid from "@/components/ui/AppGrid";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import SectionHeader from "@/components/ui/SectionHeader";
import { useCapability } from "@/hooks/useCapabilities";

import BtrfsReplicationCard from "./BtrfsReplicationCard";
import BtrfsSettingsCard from "./BtrfsSettingsCard";
import BtrfsSnapshotRulesCard from "./BtrfsSnapshotRulesCard";
import BtrfsSnapshotsCard from "./BtrfsSnapshotsCard";
import BtrfsStatusStrip from "./BtrfsStatusStrip";
import BtrfsSubvolumesCard from "./BtrfsSubvolumesCard";

interface BtrfsPageProps {
  selectedUuid?: string;
  onSelect: (uuid: string) => void;
}

// Mount point first: that is the name people know the filesystem by.
const filesystemLabel = (fs: BtrfsFilesystem) =>
  `${fs.mountpoints[0]?.mountpoint ?? ""} · ${fs.label || fs.uuid}`;

const BtrfsFilesystems = ({ selectedUuid, onSelect }: BtrfsPageProps) => {
  const { data: filesystems } = useSuspenseQuery({
    ...linuxio.storage.btrfs_list,
    refetchInterval: 10000,
  });
  const [advanced, setAdvanced] = useState(false);

  if (filesystems.length === 0) {
    return (
      <AppAlert severity="info">No btrfs filesystems are mounted.</AppAlert>
    );
  }

  const selected =
    filesystems.find((fs) => fs.uuid === selectedUuid) ?? filesystems[0];

  return (
    <>
      {filesystems.length > 1 && (
        <AppSelect
          label="Filesystem"
          onChange={(event) => onSelect(event.target.value)}
          style={{ minWidth: 320, marginBottom: "var(--app-space-16)" }}
          value={selected.uuid}
        >
          {filesystems.map((fs) => (
            <option key={fs.uuid} value={fs.uuid}>
              {filesystemLabel(fs)}
            </option>
          ))}
        </AppSelect>
      )}
      <AppGrid container spacing={2}>
        <AppGrid size={{ xs: 12 }}>
          <BtrfsStatusStrip filesystem={selected} key={selected.uuid} />
        </AppGrid>
        <AppGrid size={{ xs: 12 }}>
          <BtrfsSnapshotsCard key={selected.uuid} uuid={selected.uuid} />
        </AppGrid>
        <AppGrid size={{ xs: 12 }}>
          <BtrfsSnapshotRulesCard key={selected.uuid} uuid={selected.uuid} />
        </AppGrid>
        <AppGrid size={{ xs: 12 }}>
          <section>
            <SectionHeader
              controlsId="btrfs-advanced"
              expanded={advanced}
              onToggle={() => setAdvanced((current) => !current)}
              title="Advanced"
            />
            <AppCollapse in={advanced} unmountOnExit>
              <AppGrid container id="btrfs-advanced" spacing={2}>
                <AppGrid size={{ xs: 12 }}>
                  <BtrfsSubvolumesCard uuid={selected.uuid} />
                </AppGrid>
                <AppGrid size={{ xs: 12 }}>
                  <BtrfsReplicationCard
                    key={selected.uuid}
                    uuid={selected.uuid}
                  />
                </AppGrid>
                <AppGrid size={{ xs: 12 }}>
                  <BtrfsSettingsCard
                    key={selected.uuid}
                    mountpoint={selected.mountpoints[0]?.mountpoint}
                    uuid={selected.uuid}
                  />
                </AppGrid>
              </AppGrid>
            </AppCollapse>
          </section>
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
