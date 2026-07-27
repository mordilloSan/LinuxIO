import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import MountsPage from "./-components/MountsPage";

export const Route = createFileRoute("/_authenticated/shares/mounts")({
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.storage.list_nfs_mounts.queryOptions(),
      linuxio.storage.list_cifs_mounts.queryOptions(),
    ]),
  component: MountsPage,
});
