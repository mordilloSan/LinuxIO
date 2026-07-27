import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";

import { linuxio } from "@/api";
import { RoutedTabContainer } from "@/components/tabbar";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import { SERVICES_TABS } from "./-components/servicesTabs";
import TimersTab from "./-components/TimersTab";
import UnitViewToggle from "./-components/UnitViewToggle";

export const Route = createFileRoute("/_authenticated/services/timers")({
  validateSearch: (search) => ({
    ...optionalString(search, "timer"),
  }),
  loaderDeps: ({ search }) => ({ timer: search.timer }),
  loader: ({ context, deps, preload }) => {
    const queries: LoaderQueryOptions[] = [
      linuxio.systemd.list_timers.queryOptions(),
    ];
    if (deps.timer) {
      queries.push(linuxio.systemd.get_unit_info.queryOptions(deps.timer));
    }
    return loadRouteQueries({ context, preload }, queries);
  },
  component: TimersRoute,
});

function TimersRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
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
    <RoutedTabContainer
      containerStyle={{ paddingInline: 0 }}
      rightContent={<UnitViewToggle viewModeKey="timers.list" />}
      tabs={SERVICES_TABS}
    >
      <TimersTab onSelectedChange={setSelected} selected={search.timer} />
    </RoutedTabContainer>
  );
}
