import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { HardDriveIcon } from "@/icons/svg";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import StoragePage from "./-components/StoragePage";

export const Route = createFileRoute("/_authenticated/storage")({
  validateSearch: (search) => ({
    ...optionalString(search, "drive"),
    ...optionalString(search, "fs"),
    ...optionalString(search, "storageTab"),
  }),
  loaderDeps: ({ search }) => ({ storageTab: search.storageTab }),
  loader: ({ context, deps, preload }) => {
    const queries: LoaderQueryOptions[] =
      deps.storageTab === "lvm"
        ? [
            linuxio.storage.list_pvs.queryOptions(),
            linuxio.storage.list_vgs.queryOptions(),
            linuxio.storage.list_lvs.queryOptions(),
          ]
        : [
            linuxio.storage.get_drive_info.queryOptions(),
            linuxio.system.get_fs_info.queryOptions(),
            linuxio.storage.list_nfs_mounts.queryOptions(),
          ];

    return loadRouteQueries({ context, preload }, queries);
  },
  component: StoragePage,
  staticData: {
    navigation: {
      icon: HardDriveIcon,
      position: 40,
      title: "Storage",
    },
  },
});
