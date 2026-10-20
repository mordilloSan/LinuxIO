import { useSuspenseQueries } from "@tanstack/react-query";

import { CACHE_TTL_MS, linuxio } from "@/api";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";

import VMOverview from "./VMOverview";

const VMDashboardPage = () => {
  // VM list and preflight are polled by the /vm layout; the host snapshot
  // polls at the main dashboard's cadence while this tab is open.
  const [
    { data: vms },
    { data: preflight },
    { data: pools },
    { data: host },
    { data: live },
    { data: interfaces },
  ] = useSuspenseQueries({
    queries: [
      { ...linuxio.virt.list, refetchOnMount: false },
      { ...linuxio.virt.preflight({}), refetchOnMount: false },
      { ...linuxio.virt.pools, refetchInterval: 15000 },
      { ...linuxio.system.get_host_info, staleTime: CACHE_TTL_MS.ONE_DAY },
      {
        ...linuxio.monitoring.get_live,
        refetchInterval: DASHBOARD_REFETCH_FAST_MS,
      },
      linuxio.network.get_network_info,
    ],
  });

  return (
    <VMOverview
      hostname={host.hostname}
      interfaces={interfaces}
      live={live}
      pools={pools}
      preflight={preflight}
      vms={vms}
    />
  );
};

export default VMDashboardPage;
