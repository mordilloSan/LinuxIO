import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import DockerImagesPage from "./-components/DockerImagesPage";

export const Route = createFileRoute("/_authenticated/docker/images")({
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.docker.list_images.queryOptions(),
    ]),
  component: DockerImagesPage,
});
