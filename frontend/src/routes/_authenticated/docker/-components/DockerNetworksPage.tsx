import { Icon } from "@iconify/react";
import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import DockerNetworksTable from "./NetworkList";

const DockerNetworksPage = () => {
  const [networksView, setNetworksView] = useViewMode(
    "docker.networks",
    "table",
  );
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
        <AppButton
          onClick={createNetworkHandler}
          size="small"
          startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
          variant="contained"
        >
          Add Network
        </AppButton>
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
