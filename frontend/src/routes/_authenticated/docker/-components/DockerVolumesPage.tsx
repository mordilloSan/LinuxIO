import { Icon } from "@iconify/react";
import { useCallback, useState } from "react";

import { RoutedTabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

import { DOCKER_TABS } from "./dockerTabs";
import VolumeList from "./VolumeList";

const DockerVolumesPage = () => {
  const [volumesView, setVolumesView] = useViewMode("docker.volumes", "table");
  const [createVolumeHandler, setCreateVolumeHandler] = useState<
    (() => void) | null
  >(null);
  const handleMountCreateVolumeHandler = useCallback((handler: () => void) => {
    setCreateVolumeHandler(() => handler);
  }, []);

  const actions = (
    <>
      <AppTooltip
        title={
          volumesView === "table"
            ? "Switch to card view"
            : "Switch to table view"
        }
      >
        <AppIconButton
          onClick={() =>
            setVolumesView(volumesView === "table" ? "card" : "table")
          }
          size="small"
        >
          {volumesView === "table" ? (
            <Icon height={20} icon="mdi:card-multiple" width={20} />
          ) : (
            <Icon height={20} icon="mdi:table" width={20} />
          )}
        </AppIconButton>
      </AppTooltip>
      {createVolumeHandler && (
        <AppButton
          onClick={createVolumeHandler}
          size="small"
          startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
          variant="contained"
        >
          Add Volume
        </AppButton>
      )}
    </>
  );

  return (
    <RoutedTabContainer rightContent={actions} tabs={DOCKER_TABS}>
      <VolumeList
        onMountCreateHandler={handleMountCreateVolumeHandler}
        viewMode={volumesView}
      />
    </RoutedTabContainer>
  );
};

export default DockerVolumesPage;
