import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { ShareIcon } from "@/icons/svg";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import SharesPage from "./-components/SharesPage";

export const Route = createFileRoute("/_authenticated/shares")({
  validateSearch: (search) => ({
    ...optionalString(search, "sharesTab"),
  }),
  loaderDeps: ({ search }) => ({ sharesTab: search.sharesTab }),
  loader: ({ context, deps, preload }) => {
    const queries: LoaderQueryOptions[] =
      deps.sharesTab === "mounts"
        ? [
            linuxio.storage.list_nfs_mounts.queryOptions(),
            linuxio.storage.list_cifs_mounts.queryOptions(),
          ]
        : [
            linuxio.shares.list_nfs_shares.queryOptions(),
            linuxio.shares.list_samba_shares.queryOptions(),
          ];

    return loadRouteQueries({ context, preload }, queries);
  },
  component: SharesPage,
  staticData: {
    navigation: {
      icon: ShareIcon,
      position: 70,
      title: "Shares",
    },
  },
});
