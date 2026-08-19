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
  context: ({ deps }) => ({
    listQueryOptions: linuxio.systemd.list_sockets,
    selectedQueryOptions: deps.socket
      ? linuxio.systemd.get_unit_info({ unitName: deps.socket })
      : undefined,
  }),
  loader: (loaderArgs) => {
    const { listQueryOptions, selectedQueryOptions } = loaderArgs.context;
    const queries: LoaderQueryOptions[] = [listQueryOptions];
    if (selectedQueryOptions) queries.push(selectedQueryOptions);
    return loadRouteQueries(loaderArgs, queries);
  },
  component: SocketsRoute,
});

function SocketsRoute() {
  const search = Route.useSearch();
  const listQueryOptions = Route.useRouteContext({
    select: (context) => context.listQueryOptions,
  });
  const selectedQueryOptions = Route.useRouteContext({
    select: (context) => context.selectedQueryOptions,
  });
  const navigate = Route.useNavigate();
  const [viewMode, setViewMode] = useViewMode("sockets.list");
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
        listQueryOptions={listQueryOptions}
        selectedQueryOptions={selectedQueryOptions}
        selected={search.socket}
        viewMode={viewMode}
      />
    </>
  );
}
