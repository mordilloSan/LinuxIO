import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";

import { linuxio } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import TimersTab from "./-components/TimersTab";

export const Route = createFileRoute("/_authenticated/services/timers")({
  validateSearch: (search) => ({
    ...optionalString(search, "timer"),
  }),
  loaderDeps: ({ search }) => ({ timer: search.timer }),
  context: ({ deps }) => ({
    listQueryOptions: linuxio.systemd.list_timers,
    selectedQueryOptions: deps.timer
      ? linuxio.systemd.get_unit_info({ unitName: deps.timer })
      : undefined,
  }),
  loader: (loaderArgs) => {
    const { listQueryOptions, selectedQueryOptions } = loaderArgs.context;
    const queries: LoaderQueryOptions[] = [listQueryOptions];
    if (selectedQueryOptions) queries.push(selectedQueryOptions);
    return loadRouteQueries(loaderArgs, queries);
  },
  component: TimersRoute,
});

function TimersRoute() {
  const search = Route.useSearch();
  const listQueryOptions = Route.useRouteContext({
    select: (context) => context.listQueryOptions,
  });
  const selectedQueryOptions = Route.useRouteContext({
    select: (context) => context.selectedQueryOptions,
  });
  const navigate = Route.useNavigate();
  const [viewMode, setViewMode] = useViewMode("timers.list");
  const setSelected = useCallback(
    (timer: string | null) =>
      navigate({
        search: (previous) => ({
          ...previous,
          timer: timer ?? undefined,
        }),
        to: "/services/timers",
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
      <TimersTab
        onSelectedChange={setSelected}
        listQueryOptions={listQueryOptions}
        selectedQueryOptions={selectedQueryOptions}
        selected={search.timer}
        viewMode={viewMode}
      />
    </>
  );
}
