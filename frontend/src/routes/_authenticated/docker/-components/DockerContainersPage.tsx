import { useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useCapability } from "@/hooks/useCapabilities";
import { useViewMode } from "@/hooks/useViewMode";

import ContainerAutoUpdateDialog from "./ContainerAutoUpdateDialog";
import ContainerList from "./ContainerList";
import { useContainerAutoUpdateState } from "./useContainerAutoUpdateState";
import { useDockerUpdateCheck } from "./useDockerUpdateCheck";

const DockerContainersPage = () => {
  const { button: checkUpdatesButton, isCheckingUpdates } =
    useDockerUpdateCheck();
  const { isEnabled: dockerUpdatesEnabled, reason: dockerUpdatesReason } =
    useCapability("dockerUpdatesAvailable");
  const [autoUpdateDialogOpen, setAutoUpdateDialogOpen] = useState(false);
  const containerAutoUpdate = useContainerAutoUpdateState();
  const [containerView, setContainerView] = useViewMode(
    "docker.containers",
    "card",
  );
  const actions = (
    <>
      {checkUpdatesButton}
      <AppActionIconButton
        ariaLabel="Container auto-update settings"
        disabled={!dockerUpdatesEnabled}
        icon="mdi:cog"
        iconSize={20}
        label={
          dockerUpdatesEnabled
            ? "Container auto-update settings"
            : dockerUpdatesReason
        }
        onClick={() => setAutoUpdateDialogOpen(true)}
      />
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setContainerView}
        viewMode={containerView}
      />
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <ContainerList
        checkingUpdates={isCheckingUpdates}
        containerAutoUpdate={containerAutoUpdate}
        viewMode={containerView}
      />
      <ContainerAutoUpdateDialog
        autoUpdate={containerAutoUpdate}
        onClose={() => setAutoUpdateDialogOpen(false)}
        open={autoUpdateDialogOpen}
        dockerUpdatesEnabled={dockerUpdatesEnabled}
        dockerUpdatesReason={dockerUpdatesReason}
      />
    </>
  );
};

export default DockerContainersPage;
