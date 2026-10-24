import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import UnitKindPage from "./-components/UnitKindPage";
import { socketKind, unitListQueries } from "./-components/unitKinds";

export const Route = createFileRoute("/_authenticated/services/sockets")({
  validateSearch: (search) => ({
    ...optionalString(search, "socket"),
  }),
  loaderDeps: ({ search }) => ({ socket: search.socket }),
  context: ({ deps }) => ({
    listQueryOptions: linuxio.systemd.list_sockets,
    selectedQueryOptions: deps.socket
      ? linuxio.systemd.get_unit_info({ unitName: deps.socket })
      : undefined,
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, unitListQueries(loaderArgs.context)),
  component: SocketRoute,
});

function SocketRoute() {
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
      kind={socketKind}
      listQueryOptions={listQueryOptions}
      onSelectedChange={(socket) =>
        navigate({
          search: (previous) => ({
            ...previous,
            socket: socket ?? undefined,
          }),
          to: "/services/sockets",
        })
      }
      selected={search.socket}
      selectedQueryOptions={selectedQueryOptions}
    />
  );
}
