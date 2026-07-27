import { Icon } from "@iconify/react";
import { useCallback, useState } from "react";

import { RoutedTabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

import { DOCKER_TABS } from "./dockerTabs";
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
      <AppTooltip
        title={
          networksView === "table"
            ? "Switch to card view"
            : "Switch to table view"
        }
      >
        <AppIconButton
          onClick={() =>
            setNetworksView(networksView === "table" ? "card" : "table")
          }
          size="small"
        >
          {networksView === "table" ? (
            <Icon height={20} icon="mdi:card-multiple" width={20} />
          ) : (
            <Icon height={20} icon="mdi:table" width={20} />
          )}
        </AppIconButton>
      </AppTooltip>
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
    <RoutedTabContainer rightContent={actions} tabs={DOCKER_TABS}>
      <DockerNetworksTable
        onMountCreateHandler={handleMountCreateNetworkHandler}
        viewMode={networksView}
      />
    </RoutedTabContainer>
  );
};

export default DockerNetworksPage;
