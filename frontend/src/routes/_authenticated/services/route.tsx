import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { ServerCogIcon } from "@/icons/svg";
import { type LoaderQueryOptions, loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import ServicesPage from "./-components/ServicesPage";

export const Route = createFileRoute("/_authenticated/services")({
  validateSearch: (search) => ({
    ...optionalString(search, "section"),
    ...optionalString(search, "service"),
    ...optionalString(search, "socket"),
    ...optionalString(search, "timer"),
  }),
  loaderDeps: ({ search }) => ({
    section: search.section,
    service: search.service,
    socket: search.socket,
    timer: search.timer,
  }),
  loader: ({ context, deps, preload }) => {
    const queries: LoaderQueryOptions[] = [];
    let selectedUnit: string | undefined;

    if (deps.section === "timers") {
      queries.push(linuxio.systemd.list_timers.queryOptions());
      selectedUnit = deps.timer;
    } else if (deps.section === "sockets") {
      queries.push(linuxio.systemd.list_sockets.queryOptions());
      selectedUnit = deps.socket;
    } else {
      queries.push(linuxio.systemd.list_services.queryOptions());
      selectedUnit = deps.service;
    }

    if (selectedUnit) {
      queries.push(linuxio.systemd.get_unit_info.queryOptions(selectedUnit));
    }

    return loadRouteQueries({ context, preload }, queries);
  },
  component: ServicesPage,
  staticData: {
    navigation: {
      icon: ServerCogIcon,
      position: 30,
      title: "Services",
    },
  },
});
