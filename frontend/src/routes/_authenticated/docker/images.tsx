import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerImagesPage from "./-components/DockerImagesPage";

export const Route = createFileRoute("/_authenticated/docker/images")({
  validateSearch: (search) => ({
    ...optionalString(search, "image"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.docker.list_images]),
  component: DockerImagesPage,
});
