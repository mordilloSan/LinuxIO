import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
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
        <AppActionIconButton
          ariaLabel="Add Image"
          icon="mdi:plus"
          iconSize={20}
          label="Add Image"
          onClick={createImageHandler}
        />
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
