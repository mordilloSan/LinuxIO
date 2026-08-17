import { useCallback, useState } from "react";

import { linuxio, useCallMutation } from "@/api";
import PruneDialog, {
  type PruneOptions,
} from "@/components/docker/PruneDialog";
import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useViewMode } from "@/hooks/useViewMode";

import DockerNetworksTable from "./NetworkList";

const NETWORK_PRUNE_OPTIONS = {
  buildCache: false,
  containers: false,
  images: false,
  networks: true,
  volumes: false,
} as const;

const DockerNetworksPage = () => {
  const [networksView, setNetworksView] = useViewMode("docker.networks");
  const [createNetworkHandler, setCreateNetworkHandler] = useState<
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
  const handleMountCreateNetworkHandler = useCallback((handler: () => void) => {
    setCreateNetworkHandler(() => handler);
  }, []);

  const actions = (
    <>
      <AppActionIconButton
        ariaLabel="Prune All"
        disabled={isPruning}
        icon="mdi:broom"
        iconSize={20}
        label="Prune All"
        loading={isPruning}
        onClick={() => setPruneDialogOpen(true)}
      />
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setNetworksView}
        viewMode={networksView}
      />
      {createNetworkHandler && (
        <AppActionIconButton
          ariaLabel="Add Network"
          icon="mdi:plus"
          iconSize={20}
          label="Add Network"
          onClick={createNetworkHandler}
        />
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
      <PruneDialog
        initialOptions={NETWORK_PRUNE_OPTIONS}
        isLoading={isPruning}
        onClose={() => !isPruning && setPruneDialogOpen(false)}
        onConfirm={(options: PruneOptions) => systemPrune(options)}
        open={pruneDialogOpen}
      />
    </>
  );
};

export default DockerNetworksPage;
