import { createFileRoute, redirect } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { hasAccessPolicy } from "@/hooks/useCapabilities";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import StorageOverview from "./-components/overview/StorageOverview";
import { POOLS_ACCESS } from "./-components/pools/poolsAccess";

export const Route = createFileRoute("/_authenticated/storage/")({
  // The Disks overview lived here; old links, alerts among them, still carry
  // its selection.
  validateSearch: (search) => ({
    ...optionalString(search, "drive"),
    ...optionalString(search, "fs"),
    ...optionalString(search, "pool"),
    ...optionalString(search, "raid"),
  }),
  beforeLoad: ({ context, search }) => {
    // A session that cannot open pools stays on the Overview.
    if (search.pool && hasAccessPolicy(POOLS_ACCESS, context.access)) {
      throw redirect({
        params: { uuid: search.pool },
        replace: true,
        to: "/storage/pools/$uuid",
      });
    }
    if (search.drive || search.fs || search.raid) {
      throw redirect({
        replace: true,
        search: { drive: search.drive, fs: search.fs, raid: search.raid },
        to: "/storage/disks",
      });
    }
  },
  // The share lists answer any session; the pool lists need privilege and
  // are read by the page as it can.
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.shares.list_samba_shares,
      linuxio.shares.list_nfs_shares,
    ]),
  component: StorageOverview,
});
