import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import StorageTopologyPage from "./-components/StorageTopologyPage";

export const Route = createFileRoute("/_authenticated/storage/topology")({
  validateSearch: (search) => optionalString(search, "node"),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.storage.get_topology]),
  component: StorageTopologyRoute,
});

function StorageTopologyRoute() {
  const selection = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <StorageTopologyPage
      selection={selection}
      onSelectionChange={(search) =>
        void navigate({ search, resetScroll: false })
      }
    />
  );
}
