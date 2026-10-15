import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import HtopTab from "./-components/HtopTab";

export const Route = createFileRoute("/_authenticated/services/htop")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.monitoring.get_live,
      linuxio.monitoring.get_processes,
    ]),
  component: HtopTab,
});
