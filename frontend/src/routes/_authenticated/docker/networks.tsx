import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import DockerNetworksPage from "./-components/DockerNetworksPage";

export const Route = createFileRoute("/_authenticated/docker/networks")({
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.docker.list_networks.queryOptions(),
    ]),
  component: DockerNetworksPage,
});
