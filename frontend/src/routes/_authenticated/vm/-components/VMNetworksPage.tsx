import { useSuspenseQueries } from "@tanstack/react-query";

import { linuxio } from "@/api";

import { VMNetworksTab } from "./VMTabs";

const VMNetworksPage = () => {
  const [{ data: vms }, { data: networks }] = useSuspenseQueries({
    queries: [
      { ...linuxio.virt.list, refetchOnMount: false },
      linuxio.virt.networks,
    ],
  });

  return <VMNetworksTab networks={networks} vms={vms} />;
};

export default VMNetworksPage;
