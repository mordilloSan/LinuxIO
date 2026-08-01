import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";

import { linuxio } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import ServicesTab from "./-components/ServicesTab";

export const Route = createFileRoute("/_authenticated/services/")({
  validateSearch: (search) => ({
    ...optionalString(search, "service"),
  }),
  loaderDeps: ({ search }) => ({ service: search.service }),
  loader: (loaderArgs) => {
    const { deps } = loaderArgs;
    const queries: LoaderQueryOptions[] = [
      linuxio.systemd.list_services.queryOptions(),
    ];
    if (deps.service) {
      queries.push(linuxio.systemd.get_unit_info.queryOptions(deps.service));
    }
    return loadRouteQueries(loaderArgs, queries);
  },
  component: ServicesRoute,
});

function ServicesRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const [viewMode, setViewMode] = useViewMode("services.list", "table");
  const setSelected = useCallback(
    (service: string | null) =>
      navigate({
        search: (previous) => ({
          ...previous,
          service: service ?? undefined,
        }),
        to: "/services",
      }),
    [navigate],
  );

  return (
    <>
      <RoutedTabActions>
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setViewMode}
          viewMode={viewMode}
        />
      </RoutedTabActions>
      <ServicesTab
        onSelectedChange={setSelected}
        onViewModeChange={setViewMode}
        selected={search.service}
        viewMode={viewMode}
      />
    </>
  );
}
