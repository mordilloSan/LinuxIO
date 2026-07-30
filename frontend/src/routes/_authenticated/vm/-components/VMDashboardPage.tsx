import { useSuspenseQueries } from "@tanstack/react-query";

import { linuxio } from "@/api";

import { VMDashboardTab } from "./VMTabs";

const VMDashboardPage = () => {
  const [{ data: vms }, { data: preflight }] = useSuspenseQueries({
    queries: [
      linuxio.virt.list.queryOptions(),
      linuxio.virt.preflight.queryOptions({}),
    ],
  });

  return <VMDashboardTab preflight={preflight} vms={vms} />;
};

export default VMDashboardPage;
