import { lazy, Suspense, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

import ContainerList from "./ContainerList";
import { useDockerUpdateCheck } from "./useDockerUpdateCheck";

const ContainerFormDialog = lazy(
  () => import("@/components/docker/ContainerFormDialog"),
);

const DockerContainersPage = () => {
  const { button: checkUpdatesButton, isCheckingUpdates } =
    useDockerUpdateCheck();
  const [containerView, setContainerView] = useViewMode("docker.containers");
  const [createOpen, setCreateOpen] = useState(false);
  const actions = (
    <HeaderActions
      create={
        <AppActionIconButton
          ariaLabel="Create container"
          icon="mdi:plus"
          iconSize={20}
          label="Create container"
          onClick={() => setCreateOpen(true)}
        />
      }
      refresh={checkUpdatesButton}
      view={
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setContainerView}
          viewMode={containerView}
        />
      }
    />
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <ContainerList
        checkingUpdates={isCheckingUpdates}
        viewMode={containerView}
      />
      {createOpen && (
        <Suspense fallback={null}>
          <ContainerFormDialog
            mode="create"
            onClose={() => setCreateOpen(false)}
            open
          />
        </Suspense>
      )}
    </>
  );
};

export default DockerContainersPage;
