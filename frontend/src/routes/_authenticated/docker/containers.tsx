import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerContainersPage from "./-components/DockerContainersPage";

export const Route = createFileRoute("/_authenticated/docker/containers")({
  validateSearch: (search) => ({
    ...optionalString(search, "container"),
  }),
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.docker.list_containers.queryOptions(),
      linuxio.docker.get_container_auto_update.queryOptions(),
    ]),
  component: DockerContainersPage,
});
