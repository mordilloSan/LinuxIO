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
  loader: (loaderArgs) => {
    const { deps } = loaderArgs;
    const queries: LoaderQueryOptions[] = [
      linuxio.systemd.list_timers.queryOptions(),
    ];
    if (deps.timer) {
      queries.push(linuxio.systemd.get_unit_info.queryOptions(deps.timer));
    }
    return loadRouteQueries(loaderArgs, queries);
  },
  component: TimersRoute,
});

function TimersRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const [viewMode, setViewMode] = useViewMode("timers.list", "table");
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
        onViewModeChange={setViewMode}
        selected={search.timer}
        viewMode={viewMode}
      />
    </>
  );
}
