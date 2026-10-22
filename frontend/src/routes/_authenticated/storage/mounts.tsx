import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import MountsPage from "./-components/shares/MountsPage";

export const Route = createFileRoute("/_authenticated/storage/mounts")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.storage.list_nfs_mounts,
      linuxio.storage.list_cifs_mounts,
    ]),
  component: MountsPage,
});
