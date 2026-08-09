import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import DockerImagesPage from "./-components/DockerImagesPage";

export const Route = createFileRoute("/_authenticated/docker/images")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.docker.list_images]),
  component: DockerImagesPage,
});
