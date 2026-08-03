import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { fileBrowserListingQueryOptions } from "@/hooks/filebrowser/fileBrowserListingQueryOptions";
import { FolderIcon } from "@/icons/svg";
import { LOADER_FRESHNESS, loadRouteQueries } from "@/routes/-loader";
import {
  optionalBoolean,
  optionalNumber,
  optionalString,
} from "@/routes/-search";

import FileBrowserPage from "./-components/FileBrowserPage";

export const Route = createFileRoute("/_authenticated/filebrowser/$")({
  validateSearch: (search) => ({
    ...optionalBoolean(search, "enabled"),
    ...optionalString(search, "redirect"),
    ...optionalNumber(search, "tail"),
  }),
  loader: (loaderArgs) => {
    const { params } = loaderArgs;
    const path = params._splat ? `/${params._splat}` : "/";
    return loadRouteQueries(
      loaderArgs,
      [
        linuxio.filebrowser.resource_get.queryOptions(
          { path },
          fileBrowserListingQueryOptions,
        ),
      ],
      LOADER_FRESHNESS.BACKGROUND,
    );
  },
  component: FileBrowserPage,
  staticData: {
    navigation: {
      icon: FolderIcon,
      params: { _splat: "" },
      position: 100,
      title: "Navigator",
    },
  },
});
