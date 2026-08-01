import { Icon } from "@iconify/react";
import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
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
