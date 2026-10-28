import { useState, type ComponentType } from "react";

import { linuxio } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useViewMode } from "@/hooks/useViewMode";

import { DOCKER_TOAST_META } from "./dockerToast";
import PruneDialog, { type PruneOptions } from "./PruneDialog";

interface DockerResourcePageProps {
  /** Singular name used for the "Add <label>" action, e.g. "Image". */
  label: string;
  List: ComponentType<{
    onMountCreateHandler: (handler: () => void) => void;
    viewMode: "table" | "card";
  }>;
  /** What "Prune All" preselects: only this resource kind. */
  pruneOptions: PruneOptions;
  /** Persisted view-mode preference key, e.g. "docker.images". */
  viewModeKey: string;
}

/** Shared page shell for the Images, Networks and Volumes tabs. */
const DockerResourcePage = ({
  label,
  List,
  pruneOptions,
  viewModeKey,
}: DockerResourcePageProps) => {
  const toast = useScopedToast(DOCKER_TOAST_META);
  const [view, setView] = useViewMode(viewModeKey);
  const [pruneDialogOpen, setPruneDialogOpen] = useState(false);
  const [createHandler, setCreateHandler] = useState<(() => void) | null>(null);
  const { mutate: systemPrune, isPending: isPruning } =
    linuxio.docker.system_prune.useMutation({
      success: () => {
        toast.success("Docker prune completed");
        setPruneDialogOpen(false);
      },
      error: "Prune failed",
      toast: DOCKER_TOAST_META,
    });

  const actions = (
    <HeaderActions
      create={
        createHandler && (
          <AppActionIconButton
            ariaLabel={`Add ${label}`}
            icon="mdi:plus"
            iconSize={20}
            label={`Add ${label}`}
            onClick={createHandler}
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
          onViewModeChange={setView}
          viewMode={view}
        />
      }
    />
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <List
        onMountCreateHandler={(handler) => setCreateHandler(() => handler)}
        viewMode={view}
      />
      <PruneDialog
        isLoading={isPruning}
        onClose={() => !isPruning && setPruneDialogOpen(false)}
        onConfirm={(options: PruneOptions) => systemPrune(options)}
        initialOptions={pruneOptions}
        open={pruneDialogOpen}
      />
    </>
  );
};

export default DockerResourcePage;
