import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { RoutedTabLayout, type RoutedTabEntry } from "@/components/tabbar";
import { hasAccessPolicy, useAccessContext } from "@/hooks/useCapabilities";
import { HardDriveIcon } from "@/icons/svg";

import { POOLS_ACCESS } from "./-components/pools/poolsAccess";
import { STORAGE_TABS } from "./-components/storageTabs";
import { Route as SnapshotsRoute } from "./snapshots";

function StorageLayout() {
  const access = useAccessContext();
  const snapshotsAllowed = hasAccessPolicy(
    SnapshotsRoute.options.staticData?.access,
    access,
  );
  const poolsAllowed = hasAccessPolicy(POOLS_ACCESS, access);
  // Pools and Snapshots work on mounted btrfs filesystems; without one they are empty.
  const { data: filesystems = [] } = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: snapshotsAllowed || poolsAllowed,
  });
  const mounted = filesystems.length > 0;
  const tabs: readonly RoutedTabEntry[] = STORAGE_TABS.filter((tab) => {
    if ("items" in tab) return true;
    if (tab.to === "/storage/pools") return poolsAllowed && mounted;
    if (tab.to === "/storage/snapshots") return snapshotsAllowed && mounted;
    return true;
  });
  return (
    <RoutedTabLayout tabs={tabs}>
      <Outlet />
    </RoutedTabLayout>
  );
}

export const Route = createFileRoute("/_authenticated/storage")({
  component: StorageLayout,
  staticData: {
    navigation: {
      icon: HardDriveIcon,
      position: 40,
      title: "Storage",
    },
  },
});
