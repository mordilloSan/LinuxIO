import { createFileRoute } from "@tanstack/react-router";

import { CACHE_TTL_MS, linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import VMDashboardPage from "./-components/VMDashboardPage";

export const Route = createFileRoute("/_authenticated/vm/")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.virt.pools,
      { ...linuxio.system.get_host_info, staleTime: CACHE_TTL_MS.ONE_DAY },
      linuxio.monitoring.get_live,
      linuxio.network.get_network_info,
    ]),
  component: VMDashboardPage,
});
