import { createFileRoute } from "@tanstack/react-router";

import { CACHE_TTL_MS, linuxio } from "@/api";
import { FolderIcon } from "@/icons/svg";
import { loadRouteQueries } from "@/routes/-loader";
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
  loader: ({ context, params, preload }) => {
    const path = params._splat ? `/${params._splat}` : "/";
    return loadRouteQueries({ context, preload }, [
      linuxio.filebrowser.resource_get.queryOptions(
        { path },
        { staleTime: CACHE_TTL_MS.NONE },
      ),
    ]);
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
