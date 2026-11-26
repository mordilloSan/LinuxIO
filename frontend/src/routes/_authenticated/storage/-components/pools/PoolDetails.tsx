import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import TabSelector from "@/components/tabbar/TabSelector";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import { useCapability } from "@/hooks/useCapabilities";

import BtrfsDevicesCard from "../btrfs/BtrfsDevicesCard";
import BtrfsHealthBanner from "../btrfs/BtrfsHealthBanner";
import BtrfsSettingsCard from "../btrfs/BtrfsSettingsCard";
import BtrfsSnapshotRulesCard from "../btrfs/BtrfsSnapshotRulesCard";
import BtrfsSnapshotsCard from "../btrfs/BtrfsSnapshotsCard";
import BtrfsSpaceCard from "../btrfs/BtrfsSpaceCard";
import { describePool } from "../disk-overview/storageGraph";
import DestroyPoolDialog, { type DestroyTarget } from "./DestroyPoolDialog";
import PoolOverview from "./PoolOverview";
import { POOL_TABS, type PoolTab } from "./poolTabs";
import RenamePoolDialog from "./RenamePoolDialog";

interface PoolDetailsProps {
  /** Called once the pool is destroyed. */
  onGone: () => void;
  onTab: (tab: PoolTab) => void;
  tab: PoolTab;
  uuid: string;
}

/**
 * The pool page's panel: the health banner, then Overview, Snapshots, Disks
 * and Settings, with renaming and destroying under the menu.
 */
const PoolDetails = ({ onGone, onTab, tab, uuid }: PoolDetailsProps) => {
  const { data: filesystems = [] } = useQuery(linuxio.storage.btrfs_list);
  const index = filesystems.findIndex((fs) => fs.uuid === uuid);
  const fs = filesystems[index];
  const { data: snapshots } = useQuery({
    ...linuxio.storage.btrfs_snapshots({ uuid }),
    enabled: tab === "overview",
  });
  const { data: jobs } = useQuery({
    ...linuxio.storage.btrfs_replication_jobs,
    enabled: tab === "overview",
  });
  const { isEnabled: maintenanceAvailable } = useCapability(
    "btrfsMaintenanceAvailable",
  );
  const { data: maintenance } = useQuery({
    ...linuxio.storage.get_btrfs_maintenance,
    enabled: tab === "overview" && maintenanceAvailable,
  });
  const [menu, setMenu] = useState<HTMLElement | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [destroying, setDestroying] = useState<DestroyTarget | null>(null);

  // The layout says so when the pool is not mounted.
  if (!fs) return null;
  const name = describePool(fs, index).name;

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-12)",
      }}
    >
      <BtrfsHealthBanner filesystem={fs} key={fs.uuid} />
      <div
        style={{
          alignItems: "center",
          display: "flex",
          gap: "var(--app-space-8)",
        }}
      >
        <TabSelector onChange={onTab} options={POOL_TABS} value={tab} />
        <div style={{ marginInlineStart: "auto" }}>
          <AppActionIconButton
            icon="mdi:dots-horizontal"
            label="More pool actions"
            onClick={(event) => setMenu(event.currentTarget)}
          />
        </div>
      </div>
      <AppMenu
        anchorEl={menu}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        onClose={() => setMenu(null)}
        open={menu !== null}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        <AppMenuItem
          onClick={() => {
            setMenu(null);
            setRenaming(true);
          }}
          startAdornment={<Icon height={18} icon="mdi:pencil" width={18} />}
        >
          Rename…
        </AppMenuItem>
        <AppMenuItem
          danger
          onClick={() => {
            setMenu(null);
            setDestroying({
              uuid,
              name,
              disks: fs.devices.map(({ path }) => path).filter(Boolean),
              mounted: true,
            });
          }}
          startAdornment={<Icon height={18} icon="mdi:delete" width={18} />}
        >
          Destroy…
        </AppMenuItem>
      </AppMenu>

      {tab === "overview" && (
        <PoolOverview
          fs={fs}
          jobs={jobs}
          maintenance={maintenanceAvailable ? maintenance : null}
          onTab={onTab}
          snapshots={snapshots}
        />
      )}
      {tab === "snapshots" && (
        <>
          <BtrfsSnapshotsCard key={uuid} uuid={uuid} />
          <BtrfsSnapshotRulesCard key={`${uuid}-rules`} uuid={uuid} />
        </>
      )}
      {tab === "disks" && (
        <>
          <BtrfsDevicesCard filesystem={fs} key={uuid} />
          <BtrfsSpaceCard filesystem={fs} />
        </>
      )}
      {tab === "settings" && (
        <BtrfsSettingsCard
          key={uuid}
          mountpoint={fs.mountpoints[0]?.mountpoint}
          uuid={uuid}
        />
      )}

      {renaming && (
        <RenamePoolDialog
          label={fs.label}
          onClose={() => setRenaming(false)}
          uuid={uuid}
        />
      )}
      <DestroyPoolDialog
        onClose={() => setDestroying(null)}
        onDestroyed={onGone}
        target={destroying}
      />
    </div>
  );
};

export default PoolDetails;
