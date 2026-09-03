import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerContainersPage from "./-components/DockerContainersPage";

export const Route = createFileRoute("/_authenticated/docker/containers")({
  validateSearch: (search) => ({
    ...optionalString(search, "container"),
  }),
  loaderDeps: ({ search }) => ({ container: search.container }),
  loader: (loaderArgs) => {
    const queries: LoaderQueryOptions[] = [
      linuxio.docker.list_containers,
      linuxio.docker.get_container_auto_update,
    ];
    if (loaderArgs.deps.container) {
      queries.push(
        linuxio.docker.list_networks,
        linuxio.docker.inspect_container({
          containerId: loaderArgs.deps.container,
        }),
      );
    }
    return loadRouteQueries(loaderArgs, queries);
  },
  component: DockerContainersPage,
});
