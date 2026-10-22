import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import ProgramsTab from "./-components/ProgramsTab";

export const Route = createFileRoute("/_authenticated/services/programs")({
  validateSearch: (search) => ({ ...optionalString(search, "filter") }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.monitoring.get_programs]),
  component: ProgramsRoute,
});

function ProgramsRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <ProgramsTab
      filter={search.filter ?? ""}
      onFilterChange={(filter) =>
        void navigate({
          replace: true,
          search: (previous) => ({ ...previous, filter: filter || undefined }),
        })
      }
    />
  );
}
