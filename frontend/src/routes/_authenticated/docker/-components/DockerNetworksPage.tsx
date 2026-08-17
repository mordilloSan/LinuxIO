import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import DockerNetworksTable from "./NetworkList";

const DockerNetworksPage = () => {
  const [networksView, setNetworksView] = useViewMode("docker.networks");
  const [createNetworkHandler, setCreateNetworkHandler] = useState<
    (() => void) | null
  >(null);
  const handleMountCreateNetworkHandler = useCallback((handler: () => void) => {
    setCreateNetworkHandler(() => handler);
  }, []);

  const actions = (
    <>
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setNetworksView}
        viewMode={networksView}
      />
      {createNetworkHandler && (
        <AppActionIconButton
          ariaLabel="Add Network"
          icon="mdi:plus"
          iconSize={20}
          label="Add Network"
          onClick={createNetworkHandler}
        />
      )}
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <DockerNetworksTable
        onMountCreateHandler={handleMountCreateNetworkHandler}
        viewMode={networksView}
      />
    </>
  );
};

export default DockerNetworksPage;
