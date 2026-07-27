import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { RoutedTabContainer } from "@/components/tabbar";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DiskOverview from "./-components/DiskOverview";
import { STORAGE_TABS } from "./-components/storageTabs";

export const Route = createFileRoute("/_authenticated/storage/")({
  validateSearch: (search) => ({
    ...optionalString(search, "drive"),
    ...optionalString(search, "fs"),
  }),
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.storage.get_drive_info.queryOptions(),
      linuxio.system.get_fs_info.queryOptions(),
      linuxio.storage.list_nfs_mounts.queryOptions(),
    ]),
  component: StorageDisksRoute,
});

function StorageDisksRoute() {
  return (
    <RoutedTabContainer tabs={STORAGE_TABS}>
      <DiskOverview />
    </RoutedTabContainer>
  );
}
