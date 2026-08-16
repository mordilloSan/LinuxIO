import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";

import { linuxio } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import SocketsTab from "./-components/SocketsTab";

export const Route = createFileRoute("/_authenticated/services/sockets")({
  validateSearch: (search) => ({
    ...optionalString(search, "socket"),
  }),
  loaderDeps: ({ search }) => ({ socket: search.socket }),
  loader: (loaderArgs) => {
    const { deps } = loaderArgs;
    const queries: LoaderQueryOptions[] = [linuxio.systemd.list_sockets];
    if (deps.socket) {
      queries.push(linuxio.systemd.get_unit_info({ unitName: deps.socket }));
    }
    return loadRouteQueries(loaderArgs, queries);
  },
  component: SocketsRoute,
});

function SocketsRoute() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const [viewMode, setViewMode] = useViewMode("sockets.list", "table");
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
    <>
      <RoutedTabActions>
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setViewMode}
          viewMode={viewMode}
        />
      </RoutedTabActions>
      <SocketsTab
        onSelectedChange={setSelected}
        selected={search.socket}
        viewMode={viewMode}
      />
    </>
  );
}
