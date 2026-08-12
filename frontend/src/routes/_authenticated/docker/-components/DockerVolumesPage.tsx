import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

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
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setVolumesView}
        viewMode={volumesView}
      />
      {createVolumeHandler && (
        <AppActionIconButton
          ariaLabel="Add Volume"
          icon="mdi:plus"
          iconSize={20}
          label="Add Volume"
          onClick={createVolumeHandler}
        />
      )}
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <VolumeList
        onMountCreateHandler={handleMountCreateVolumeHandler}
        viewMode={volumesView}
      />
    </>
  );
};

export default DockerVolumesPage;
