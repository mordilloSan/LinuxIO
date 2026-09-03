import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerNetworksPage from "./-components/DockerNetworksPage";

export const Route = createFileRoute("/_authenticated/docker/networks")({
  validateSearch: (search) => ({
    ...optionalString(search, "network"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.docker.list_networks,
      linuxio.docker.list_containers,
    ]),
  component: DockerNetworksPage,
});
