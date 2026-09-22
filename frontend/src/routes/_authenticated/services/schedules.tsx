import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { requireAccess } from "@/routes/-auth";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import SchedulesTab from "./-components/SchedulesTab";

const access = { requiresPrivileged: true } satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/services/schedules")({
  validateSearch: (search) => ({ ...optionalString(search, "schedule") }),
  loaderDeps: ({ search }) => ({ schedule: search.schedule }),
  context: ({ deps }) => ({
    listQueryOptions: linuxio.schedules.list,
    selectedQueryOptions: deps.schedule
      ? linuxio.schedules.get({ id: deps.schedule })
      : undefined,
  }),
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) => {
    const queries: LoaderQueryOptions[] = [loaderArgs.context.listQueryOptions];
    if (loaderArgs.context.selectedQueryOptions)
      queries.push(loaderArgs.context.selectedQueryOptions);
    return loadRouteQueries(loaderArgs, queries);
  },
  component: SchedulesRoute,
  staticData: { access },
});

function SchedulesRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const context = Route.useRouteContext();
  return (
    <SchedulesTab
      listQueryOptions={context.listQueryOptions}
      selected={search.schedule}
      selectedQueryOptions={context.selectedQueryOptions}
      onSelectedChange={(schedule) =>
        navigate({
          to: "/services/schedules",
          search: { schedule: schedule ?? undefined },
        })
      }
    />
  );
}
