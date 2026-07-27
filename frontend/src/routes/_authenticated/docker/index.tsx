import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import DockerDashboardPage from "./-components/DockerDashboardPage";

export const Route = createFileRoute("/_authenticated/docker/")({
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.docker.list_containers.queryOptions(),
      linuxio.docker.list_images.queryOptions(),
      linuxio.docker.list_networks.queryOptions(),
      linuxio.docker.list_volumes.queryOptions(),
      linuxio.docker.get_docker_info.queryOptions(),
    ]),
  component: DockerDashboardPage,
});
