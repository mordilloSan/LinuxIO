import { useVMRouteData } from "./VMPage";
import { VMNetworksTab } from "./VMTabs";

const VMNetworksPage = () => {
  const { vms } = useVMRouteData();
  return <VMNetworksTab vms={vms} />;
};

export default VMNetworksPage;
