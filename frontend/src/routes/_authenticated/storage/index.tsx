import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DiskOverview from "./-components/DiskOverview";

export const Route = createFileRoute("/_authenticated/storage/")({
  validateSearch: (search) => ({
    ...optionalString(search, "drive"),
    ...optionalString(search, "fs"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.storage.get_drive_info.queryOptions(),
      linuxio.system.get_fs_info.queryOptions(),
      linuxio.storage.list_nfs_mounts.queryOptions(),
    ]),
  component: StorageDisksRoute,
});

function StorageDisksRoute() {
  return <DiskOverview />;
}
