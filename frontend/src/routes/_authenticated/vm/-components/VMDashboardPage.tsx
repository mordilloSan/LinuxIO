import { useSuspenseQueries } from "@tanstack/react-query";

import { linuxio } from "@/api";

import { VMDashboardTab } from "./VMTabs";

const VMDashboardPage = () => {
  const [{ data: vms }, { data: preflight }] = useSuspenseQueries({
    queries: [
      { ...linuxio.virt.list, refetchOnMount: false },
      { ...linuxio.virt.preflight({}), refetchOnMount: false },
    ],
  });

  return <VMDashboardTab preflight={preflight} vms={vms} />;
};

export default VMDashboardPage;
