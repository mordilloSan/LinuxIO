import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import UpdatesPage from "./-components/UpdatesPage";

export const Route = createFileRoute("/_authenticated/updates/")({
  loader: ({ context, preload }) => {
    if (context.access.packageKitAvailable !== true) return;
    return loadRouteQueries({ context, preload }, [
      linuxio.updates.get_updates_basic.queryOptions(),
    ]);
  },
  component: UpdatesPage,
});
