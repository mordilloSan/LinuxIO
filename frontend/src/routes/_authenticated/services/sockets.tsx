import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";

import { linuxio } from "@/api";
import { RoutedTabContainer } from "@/components/tabbar";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import { SERVICES_TABS } from "./-components/servicesTabs";
import SocketsTab from "./-components/SocketsTab";
import UnitViewToggle from "./-components/UnitViewToggle";

export const Route = createFileRoute("/_authenticated/services/sockets")({
  validateSearch: (search) => ({
    ...optionalString(search, "socket"),
  }),
  loaderDeps: ({ search }) => ({ socket: search.socket }),
  loader: ({ context, deps, preload }) => {
    const queries: LoaderQueryOptions[] = [
      linuxio.systemd.list_sockets.queryOptions(),
    ];
    if (deps.socket) {
      queries.push(linuxio.systemd.get_unit_info.queryOptions(deps.socket));
    }
    return loadRouteQueries({ context, preload }, queries);
  },
  component: SocketsRoute,
});

function SocketsRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const setSelected = useCallback(
    (socket: string | null) =>
      navigate({
        search: (previous) => ({
          ...previous,
          socket: socket ?? undefined,
        }),
        to: "/services/sockets",
      }),
    [navigate],
  );

  return (
    <RoutedTabContainer
      containerStyle={{ paddingInline: 0 }}
      rightContent={<UnitViewToggle viewModeKey="sockets.list" />}
      tabs={SERVICES_TABS}
    >
      <SocketsTab onSelectedChange={setSelected} selected={search.socket} />
    </RoutedTabContainer>
  );
}
