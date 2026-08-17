import { RoutedTabActions } from "@/components/tabbar";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import ContainerList from "./ContainerList";
import { useDockerUpdateCheck } from "./useDockerUpdateCheck";

const DockerContainersPage = () => {
  const { button: checkUpdatesButton, isCheckingUpdates } =
    useDockerUpdateCheck();
  const [containerView, setContainerView] = useViewMode("docker.containers");
  const actions = (
    <>
      {checkUpdatesButton}
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setContainerView}
        viewMode={containerView}
      />
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <ContainerList
        checkingUpdates={isCheckingUpdates}
        viewMode={containerView}
      />
    </>
  );
};

export default DockerContainersPage;
