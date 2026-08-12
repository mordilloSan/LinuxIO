import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import DockerVolumesPage from "./-components/DockerVolumesPage";

export const Route = createFileRoute("/_authenticated/docker/volumes")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.docker.list_volumes]),
  component: DockerVolumesPage,
});
