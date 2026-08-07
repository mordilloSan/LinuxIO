import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import DockerComposePage from "./-components/DockerComposePage";

export const Route = createFileRoute("/_authenticated/docker/compose")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.docker.list_compose_projects.queryOptions(),
    ]),
  component: DockerComposePage,
});
