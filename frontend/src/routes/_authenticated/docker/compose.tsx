import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerComposePage from "./-components/DockerComposePage";

export const Route = createFileRoute("/_authenticated/docker/compose")({
  validateSearch: (search) => ({
    ...optionalString(search, "stack"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.docker.list_compose_projects]),
  component: DockerComposePage,
});
