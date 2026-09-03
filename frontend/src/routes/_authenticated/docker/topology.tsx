import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import DockerTopologyPage from "./-components/DockerTopologyPage";

export const Route = createFileRoute("/_authenticated/docker/topology")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.docker.list_containers,
      linuxio.docker.list_networks,
    ]),
  component: DockerTopologyPage,
});
