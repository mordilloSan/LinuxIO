import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import SharesPage from "./-components/SharesPage";

export const Route = createFileRoute("/_authenticated/shares/")({
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.shares.list_nfs_shares.queryOptions(),
      linuxio.shares.list_samba_shares.queryOptions(),
    ]),
  component: SharesPage,
});
