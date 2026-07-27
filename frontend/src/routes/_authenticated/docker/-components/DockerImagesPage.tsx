import { Icon } from "@iconify/react";
import { useCallback, useState } from "react";

import { RoutedTabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

import { DOCKER_TABS } from "./dockerTabs";
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
      <AppTooltip
        title={
          imagesView === "table"
            ? "Switch to card view"
            : "Switch to table view"
        }
      >
        <AppIconButton
          onClick={() =>
            setImagesView(imagesView === "table" ? "card" : "table")
          }
          size="small"
        >
          {imagesView === "table" ? (
            <Icon height={20} icon="mdi:card-multiple" width={20} />
          ) : (
            <Icon height={20} icon="mdi:table" width={20} />
          )}
        </AppIconButton>
      </AppTooltip>
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
    <RoutedTabContainer rightContent={actions} tabs={DOCKER_TABS}>
      <ImageList
        onMountCreateHandler={handleMountCreateImageHandler}
        viewMode={imagesView}
      />
    </RoutedTabContainer>
  );
};

export default DockerImagesPage;
