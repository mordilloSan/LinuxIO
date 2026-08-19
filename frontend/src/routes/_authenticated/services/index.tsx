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
  context: ({ deps }) => ({
    listQueryOptions: linuxio.systemd.list_services,
    selectedQueryOptions: deps.service
      ? linuxio.systemd.get_unit_info({ unitName: deps.service })
      : undefined,
  }),
  loader: (loaderArgs) => {
    const { listQueryOptions, selectedQueryOptions } = loaderArgs.context;
    const queries: LoaderQueryOptions[] = [listQueryOptions];
    if (selectedQueryOptions) queries.push(selectedQueryOptions);
    return loadRouteQueries(loaderArgs, queries);
  },
  component: ServicesRoute,
});

function ServicesRoute() {
  const search = Route.useSearch();
  const listQueryOptions = Route.useRouteContext({
    select: (context) => context.listQueryOptions,
  });
  const selectedQueryOptions = Route.useRouteContext({
    select: (context) => context.selectedQueryOptions,
  });
  const navigate = Route.useNavigate();
  const [viewMode, setViewMode] = useViewMode("services.list");
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
        listQueryOptions={listQueryOptions}
        selectedQueryOptions={selectedQueryOptions}
        selected={search.service}
        viewMode={viewMode}
      />
    </>
  );
}
