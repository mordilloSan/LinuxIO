import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import { validateProcessSearch } from "./-components/processData";
import ProcessesPage from "./-components/ProcessesPage";

export const Route = createFileRoute("/_authenticated/processes/")({
  validateSearch: validateProcessSearch,
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.monitoring.get_processes,
      linuxio.monitoring.get_programs,
    ]),
  component: ProcessesRoute,
});

function ProcessesRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const view = search.view ?? "processes";
  const filter = search.filter ?? "";

  return (
    <ProcessesPage
      filter={filter}
      onFilterChange={(nextFilter) =>
        void navigate({
          replace: true,
          search: (previous) => ({
            ...previous,
            filter: nextFilter || undefined,
          }),
        })
      }
      onViewChange={(nextView) =>
        void navigate({
          search: (previous) => ({
            ...previous,
            view: nextView === "programs" ? nextView : undefined,
          }),
        })
      }
      view={view}
    />
  );
}
