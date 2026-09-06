import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerTopologyPage from "./-components/DockerTopologyPage";

export const Route = createFileRoute("/_authenticated/docker/topology")({
  validateSearch: (search) => ({
    ...optionalString(search, "container"),
    ...optionalString(search, "network"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.docker.list_containers,
      linuxio.docker.list_networks,
    ]),
  component: DockerTopologyRoute,
});

function DockerTopologyRoute() {
  const selection = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <DockerTopologyPage
      onSelectionChange={(search) =>
        void navigate({ search, resetScroll: false })
      }
      selection={selection}
    />
  );
}
