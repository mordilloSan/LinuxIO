import { useCallback, useState } from "react";

import { linuxio, useCallMutation } from "@/api";
import PruneDialog, {
  type PruneOptions,
} from "@/components/docker/PruneDialog";
import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useViewMode } from "@/hooks/useViewMode";

import ImageList from "./ImageList";

const IMAGE_PRUNE_OPTIONS = {
  buildCache: false,
  containers: false,
  images: true,
  networks: false,
  volumes: false,
} as const;

const DockerImagesPage = () => {
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  const [imagesView, setImagesView] = useViewMode("docker.images");
  const [pruneDialogOpen, setPruneDialogOpen] = useState(false);
  const [createImageHandler, setCreateImageHandler] = useState<
    (() => void) | null
  >(null);
  const handleMountCreateImageHandler = useCallback((handler: () => void) => {
    setCreateImageHandler(() => handler);
  }, []);
  const { mutate: systemPrune, isPending: isPruning } = useCallMutation(
    linuxio.docker.system_prune,
    {
      success: () => {
        toast.success("Docker prune completed");
        setPruneDialogOpen(false);
      },
      error: "Prune failed",
      toast: { label: "Open Docker", to: "/docker" },
    },
  );

  const actions = (
    <HeaderActions
      create={
        createImageHandler && (
          <AppActionIconButton
            ariaLabel="Add Image"
            icon="mdi:plus"
            iconSize={20}
            label="Add Image"
            onClick={createImageHandler}
          />
        )
      }
      maintenance={
        <AppActionIconButton
          ariaLabel="Prune All"
          disabled={isPruning}
          icon="mdi:broom"
          iconSize={20}
          label="Prune All"
          loading={isPruning}
          onClick={() => setPruneDialogOpen(true)}
        />
      }
      view={
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setImagesView}
          viewMode={imagesView}
        />
      }
    />
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <ImageList
        onMountCreateHandler={handleMountCreateImageHandler}
        viewMode={imagesView}
      />
      <PruneDialog
        isLoading={isPruning}
        onClose={() => !isPruning && setPruneDialogOpen(false)}
        onConfirm={(options: PruneOptions) => systemPrune(options)}
        initialOptions={IMAGE_PRUNE_OPTIONS}
        open={pruneDialogOpen}
      />
    </>
  );
};

export default DockerImagesPage;
