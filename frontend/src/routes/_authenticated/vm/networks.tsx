import { createFileRoute } from "@tanstack/react-router";

import { CACHE_TTL_MS, linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import VMNetworksPage from "./-components/VMNetworksPage";

export const Route = createFileRoute("/_authenticated/vm/networks")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.virt.networks,
      { ...linuxio.system.get_host_info, staleTime: CACHE_TTL_MS.ONE_DAY },
      linuxio.network.get_network_info,
      linuxio.monitoring.get_live,
    ]),
  component: VMNetworksPage,
});
