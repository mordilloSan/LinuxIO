import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { RefreshCcwIcon } from "@/icons/svg";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import UpdatesPage from "./-components/UpdatesPage";

export const Route = createFileRoute("/_authenticated/updates")({
  validateSearch: (search) => ({
    ...optionalString(search, "updateTab"),
  }),
  loaderDeps: ({ search }) => ({ updateTab: search.updateTab }),
  loader: ({ context, deps, preload }) => {
    if (context.access.packageKitAvailable !== true) return;

    const queries: LoaderQueryOptions[] = [
      linuxio.updates.get_updates_basic.queryOptions(),
    ];
    if (deps.updateTab === "history") {
      queries.push(linuxio.updates.get_update_history.queryOptions());
    }

    return loadRouteQueries({ context, preload }, queries);
  },
  component: UpdatesPage,
  staticData: {
    navigation: {
      icon: RefreshCcwIcon,
      position: 20,
      title: "Updates",
    },
  },
});
