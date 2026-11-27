import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import UpdatesPage from "./-components/UpdatesPage";

export const Route = createFileRoute("/_authenticated/updates/")({
  // The update whose details and changelog are open.
  validateSearch: (search) => ({ ...optionalString(search, "package") }),
  loader: (loaderArgs) => {
    const { context } = loaderArgs;
    if (context.access.packageKitAvailable !== true) return;
    return loadRouteQueries(loaderArgs, [linuxio.updates.get_updates_basic]);
  },
  component: UpdatesPage,
});
