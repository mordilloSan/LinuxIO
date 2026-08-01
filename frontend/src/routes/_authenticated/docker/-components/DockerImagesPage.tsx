import { Icon } from "@iconify/react";
import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import ImageList from "./ImageList";
import { useDockerUpdateCheck } from "./useDockerUpdateCheck";

const DockerImagesPage = () => {
  const { button: checkUpdatesButton } = useDockerUpdateCheck();
  const [imagesView, setImagesView] = useViewMode("docker.images", "table");
  const [createImageHandler, setCreateImageHandler] = useState<
    (() => void) | null
  >(null);
  const handleMountCreateImageHandler = useCallback((handler: () => void) => {
    setCreateImageHandler(() => handler);
  }, []);

  const actions = (
    <>
      {checkUpdatesButton}
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setImagesView}
        viewMode={imagesView}
      />
      {createImageHandler && (
        <AppButton
          onClick={createImageHandler}
          size="small"
          startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
          variant="contained"
        >
          Add Image
        </AppButton>
      )}
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <ImageList
        onMountCreateHandler={handleMountCreateImageHandler}
        viewMode={imagesView}
      />
    </>
  );
};

export default DockerImagesPage;
