import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import UnitKindPage from "./-components/UnitKindPage";
import { serviceKind, unitListQueries } from "./-components/unitKinds";

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
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, unitListQueries(loaderArgs.context)),
  component: ServiceRoute,
});

function ServiceRoute() {
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
      kind={serviceKind}
      listQueryOptions={listQueryOptions}
      onSelectedChange={(service) =>
        navigate({
          search: (previous) => ({
            ...previous,
            service: service ?? undefined,
          }),
          to: "/services",
        })
      }
      selected={search.service}
      selectedQueryOptions={selectedQueryOptions}
    />
  );
}
