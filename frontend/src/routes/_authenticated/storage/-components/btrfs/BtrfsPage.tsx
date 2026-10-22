import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useState, type CSSProperties } from "react";

import { linuxio, type BtrfsFilesystem } from "@/api";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppCollapse from "@/components/ui/AppCollapse";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import SectionHeader from "@/components/ui/SectionHeader";
import { useCapability } from "@/hooks/useCapabilities";
import {
  STATUS_COLOR,
  Tile,
} from "@/routes/_authenticated/vm/-components/vmCards";
import { DASHBOARD_CARD_GAP } from "@/theme/constants";
import { formatRelativeAge } from "@/utils/formaters";

import BtrfsReplicationCard from "./BtrfsReplicationCard";
import { isSystemSubvolume } from "./btrfsShared";
import BtrfsSnapshotRulesCard from "./BtrfsSnapshotRulesCard";
import BtrfsSnapshotsCard from "./BtrfsSnapshotsCard";
import BtrfsSubvolumesCard from "./BtrfsSubvolumesCard";

interface BtrfsPageProps {
  selectedUuid?: string;
  onSelect: (uuid: string) => void;
}

const pageStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-16)",
  minHeight: 0,
};

const tileGridStyle: CSSProperties = {
  display: "grid",
  gap: DASHBOARD_CARD_GAP,
  gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))",
};

// Mount point first: that is the name people know the filesystem by.
const filesystemLabel = (fs: BtrfsFilesystem) =>
  `${fs.mountpoints[0]?.mountpoint ?? ""} · ${fs.label || fs.uuid}`;

/** Snapshots and protection at a glance. */
const BtrfsTiles = ({ uuid }: { uuid: string }) => {
  // The same queries the cards below use, so the cache is shared.
  const { data: snapshots = [] } = useQuery(
    linuxio.storage.btrfs_snapshots({ uuid }),
  );
  const { data: subvolumes = [] } = useQuery(
    linuxio.storage.btrfs_subvolumes({ uuid }),
  );
  const { data: rulesData } = useQuery(linuxio.storage.btrfs_snapshot_rules);

  const latest = snapshots.reduce(
    (max, s) => Math.max(max, new Date(s.createdAt).getTime()),
    0,
  );
  const folders = subvolumes.filter(
    (s) => !s.isSnapshot && !isSystemSubvolume(s.path),
  ).length;
  const protectedFolders = (rulesData?.rules ?? []).filter(
    (rule) => rule.uuid === uuid && rule.enabled,
  ).length;
  const protection =
    protectedFolders === 0
      ? { status: "None protected", color: STATUS_COLOR.warning }
      : rulesData?.timerEnabled === false
        ? { status: "Timer off", color: STATUS_COLOR.warning }
        : { status: `of ${folders} folders`, color: STATUS_COLOR.ok };

  return (
    <div style={tileGridStyle}>
      <Tile
        count={snapshots.length}
        icon="mdi:camera-burst"
        label="Snapshots"
        status={latest > 0 ? `Last ${formatRelativeAge(latest)}` : "None yet"}
        statusColor={latest > 0 ? STATUS_COLOR.ok : STATUS_COLOR.muted}
      />
      <Tile
        count={protectedFolders}
        icon="mdi:camera-timer"
        label="Protected folders"
        status={protection.status}
        statusColor={protection.color}
      />
    </div>
  );
};

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
    <div style={pageStyle}>
      {filesystems.length > 1 && (
        <AppSelect
          label="Filesystem"
          onChange={(event) => onSelect(event.target.value)}
          style={{ alignSelf: "flex-start", minWidth: 320 }}
          value={selected.uuid}
        >
          {filesystems.map((fs) => (
            <option key={fs.uuid} value={fs.uuid}>
              {filesystemLabel(fs)}
            </option>
          ))}
        </AppSelect>
      )}
      <BtrfsTiles uuid={selected.uuid} />
      <BtrfsSnapshotsCard
        key={`snapshots-${selected.uuid}`}
        uuid={selected.uuid}
      />
      <BtrfsSnapshotRulesCard
        key={`rules-${selected.uuid}`}
        uuid={selected.uuid}
      />
      <section>
        <SectionHeader
          controlsId="btrfs-advanced"
          expanded={advanced}
          onToggle={() => setAdvanced((current) => !current)}
          title="Advanced"
        />
        <AppCollapse in={advanced} unmountOnExit>
          <div id="btrfs-advanced" style={pageStyle}>
            <BtrfsSubvolumesCard uuid={selected.uuid} />
            <BtrfsReplicationCard key={selected.uuid} uuid={selected.uuid} />
          </div>
        </AppCollapse>
      </section>
    </div>
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
