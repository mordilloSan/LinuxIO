import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import ProcessesTab from "./-components/ProcessesTab";

export const Route = createFileRoute("/_authenticated/services/processes")({
  validateSearch: (search) => ({ ...optionalString(search, "filter") }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.monitoring.get_processes]),
  component: ProcessesRoute,
});

function ProcessesRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <ProcessesTab
      filter={search.filter ?? ""}
      onFilterChange={(filter) =>
        void navigate({
          replace: true,
          search: (previous) => ({ ...previous, filter: filter || undefined }),
        })
      }
      view="processes"
    />
  );
}
