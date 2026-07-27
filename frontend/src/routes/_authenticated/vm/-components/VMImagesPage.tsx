import { useVMRouteData } from "./VMPage";
import { VMImagesTab } from "./VMTabs";

const VMImagesPage = () => {
  const { preflight } = useVMRouteData();
  return <VMImagesTab preflight={preflight} />;
};

export default VMImagesPage;
