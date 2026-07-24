import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { DockerIcon } from "@/icons/svg";
import { requireAccess } from "@/routes/-auth";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerPage from "./-components/DockerPage";

const access = {
  requiredCapabilities: ["dockerAvailable"],
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/docker")({
  validateSearch: (search) => ({
    ...optionalString(search, "container"),
    ...optionalString(search, "dockerTab"),
  }),
  loaderDeps: ({ search }) => ({ dockerTab: search.dockerTab }),
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: ({ context, deps, preload }) => {
    const queries: LoaderQueryOptions[] = [];

    switch (deps.dockerTab) {
      case "containers":
        queries.push(
          linuxio.docker.list_containers.queryOptions(),
          linuxio.docker.get_container_auto_update.queryOptions(),
        );
        break;
      case "compose":
        queries.push(linuxio.docker.list_compose_projects.queryOptions());
        break;
      case "networks":
        queries.push(linuxio.docker.list_networks.queryOptions());
        break;
      case "volumes":
        queries.push(linuxio.docker.list_volumes.queryOptions());
        break;
      case "images":
        queries.push(linuxio.docker.list_images.queryOptions());
        break;
      default:
        queries.push(
          linuxio.docker.list_containers.queryOptions(),
          linuxio.docker.list_images.queryOptions(),
          linuxio.docker.list_networks.queryOptions(),
          linuxio.docker.list_volumes.queryOptions(),
          linuxio.docker.get_docker_info.queryOptions(),
        );
    }

    return loadRouteQueries({ context, preload }, queries);
  },
  component: DockerPage,
  staticData: {
    access,
    navigation: {
      icon: DockerIcon,
      position: 50,
      title: "Docker",
    },
  },
});
