import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";

import { VMNetworksTab } from "./VMTabs";

const VMNetworksPage = () => {
  const { data: vms } = useSuspenseQuery(linuxio.virt.list.queryOptions());

  return <VMNetworksTab vms={vms} />;
};

export default VMNetworksPage;
