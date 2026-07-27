import { useVMRouteData } from "./VMPage";
import { VMDashboardTab } from "./VMTabs";

const VMDashboardPage = () => {
  const { preflight, vms } = useVMRouteData();
  return <VMDashboardTab preflight={preflight} vms={vms} />;
};

export default VMDashboardPage;
