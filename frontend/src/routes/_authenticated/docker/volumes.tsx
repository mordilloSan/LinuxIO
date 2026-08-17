import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerVolumesPage from "./-components/DockerVolumesPage";

export const Route = createFileRoute("/_authenticated/docker/volumes")({
  validateSearch: (search) => ({
    ...optionalString(search, "volume"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.docker.list_volumes]),
  component: DockerVolumesPage,
});
