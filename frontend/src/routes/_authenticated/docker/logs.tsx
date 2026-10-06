import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import DockerLogsPage from "./-components/DockerLogsPage";

export const Route = createFileRoute("/_authenticated/docker/logs")({
  validateSearch: (search) => ({
    ...optionalString(search, "container"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.docker.list_containers]),
  component: DockerLogsRoute,
});

function DockerLogsRoute() {
  const { container } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <DockerLogsPage
      container={container}
      onContainerChange={(next) =>
        void navigate({
          replace: true,
          resetScroll: false,
          search: next ? { container: next } : {},
        })
      }
    />
  );
}
