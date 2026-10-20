import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import SharesPage from "./-components/SharesPage";

export const Route = createFileRoute("/_authenticated/shares/")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.shares.list_nfs_shares,
      linuxio.shares.list_samba_shares,
    ]),
  component: SharesPage,
});
