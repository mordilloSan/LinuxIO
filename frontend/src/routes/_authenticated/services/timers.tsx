import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import UnitKindPage from "./-components/UnitKindPage";
import { timerKind, unitListQueries } from "./-components/unitKinds";

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
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, unitListQueries(loaderArgs.context)),
  component: TimerRoute,
});

function TimerRoute() {
  const search = Route.useSearch();
  const listQueryOptions = Route.useRouteContext({
    select: (context) => context.listQueryOptions,
  });
  const selectedQueryOptions = Route.useRouteContext({
    select: (context) => context.selectedQueryOptions,
  });
  const navigate = Route.useNavigate();

  return (
    <UnitKindPage
      kind={timerKind}
      listQueryOptions={listQueryOptions}
      onSelectedChange={(timer) =>
        navigate({
          search: (previous) => ({
            ...previous,
            timer: timer ?? undefined,
          }),
          to: "/services/timers",
        })
      }
      selected={search.timer}
      selectedQueryOptions={selectedQueryOptions}
    />
  );
}
