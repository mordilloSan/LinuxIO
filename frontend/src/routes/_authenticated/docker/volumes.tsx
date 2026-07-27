import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import DockerVolumesPage from "./-components/DockerVolumesPage";

export const Route = createFileRoute("/_authenticated/docker/volumes")({
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.docker.list_volumes.queryOptions(),
    ]),
  component: DockerVolumesPage,
});
