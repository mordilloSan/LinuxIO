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

import VolumeList from "./VolumeList";

const VOLUME_PRUNE_OPTIONS = {
  buildCache: false,
  containers: false,
  images: false,
  networks: false,
  volumes: true,
} as const;

const DockerVolumesPage = () => {
  const [volumesView, setVolumesView] = useViewMode("docker.volumes");
  const [createVolumeHandler, setCreateVolumeHandler] = useState<
    (() => void) | null
  >(null);
  const [pruneDialogOpen, setPruneDialogOpen] = useState(false);
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
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
  const handleMountCreateVolumeHandler = useCallback((handler: () => void) => {
    setCreateVolumeHandler(() => handler);
  }, []);

  const actions = (
    <HeaderActions
      create={
        createVolumeHandler && (
          <AppActionIconButton
            ariaLabel="Add Volume"
            icon="mdi:plus"
            iconSize={20}
            label="Add Volume"
            onClick={createVolumeHandler}
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
          onViewModeChange={setVolumesView}
          viewMode={volumesView}
        />
      }
    />
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <VolumeList
        onMountCreateHandler={handleMountCreateVolumeHandler}
        viewMode={volumesView}
      />
      <PruneDialog
        initialOptions={VOLUME_PRUNE_OPTIONS}
        isLoading={isPruning}
        onClose={() => !isPruning && setPruneDialogOpen(false)}
        onConfirm={(options: PruneOptions) => systemPrune(options)}
        open={pruneDialogOpen}
      />
    </>
  );
};

export default DockerVolumesPage;
