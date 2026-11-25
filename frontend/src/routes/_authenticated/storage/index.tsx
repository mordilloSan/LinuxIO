import { createFileRoute, redirect } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { hasAccessPolicy } from "@/hooks/useCapabilities";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DiskOverview from "./-components/disk-overview/DiskOverview";
import { POOLS_ACCESS } from "./-components/pools/poolsAccess";

export const Route = createFileRoute("/_authenticated/storage/")({
  validateSearch: (search) => ({
    ...optionalString(search, "drive"),
    ...optionalString(search, "fs"),
    ...optionalString(search, "pool"),
    ...optionalString(search, "raid"),
  }),
  // Pool details moved to their own page; alerts still link here by ?pool=.
  // A session that cannot open pools stays on the Disks overview.
  beforeLoad: ({ context, search }) => {
    if (search.pool && hasAccessPolicy(POOLS_ACCESS, context.access)) {
      throw redirect({
        params: { uuid: search.pool },
        replace: true,
        to: "/storage/pools/$uuid",
      });
    }
  },
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.storage.get_drive_info,
      {
        ...linuxio.monitoring.get_live,
        select: (live) => live.filesystems ?? [],
      },
      linuxio.storage.list_nfs_mounts,
    ]),
  component: StorageDisksRoute,
});

function StorageDisksRoute() {
  return <DiskOverview />;
}
