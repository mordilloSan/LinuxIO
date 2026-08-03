import { Icon } from "@iconify/react";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useState } from "react";

import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useCapability } from "@/hooks/useCapabilities";
import { useViewMode } from "@/hooks/useViewMode";

import ContainerAutoUpdateDialog from "./ContainerAutoUpdateDialog";
import ContainerList from "./ContainerList";
import { useContainerAutoUpdateState } from "./useContainerAutoUpdateState";
import { useDockerUpdateCheck } from "./useDockerUpdateCheck";

const dockerContainersRouteApi = getRouteApi(
  "/_authenticated/docker/containers",
);

const DockerContainersPage = () => {
  const navigate = dockerContainersRouteApi.useNavigate();
  const { button: checkUpdatesButton, isCheckingUpdates } =
    useDockerUpdateCheck();
  const { isEnabled: watchtowerEnabled, reason: watchtowerReason } =
    useCapability("watchtowerAvailable");
  const [autoUpdateDialogOpen, setAutoUpdateDialogOpen] = useState(false);
  const containerAutoUpdate = useContainerAutoUpdateState();
  const [containerView, setContainerView] = useViewMode(
    "docker.containers",
    "card",
  );
  const [containerEditMode, setContainerEditMode] = useState(false);
  const handleToggleContainerEditMode = useCallback(() => {
    if (!containerEditMode) {
      navigate({
        search: (previous) => ({
          ...previous,
          container: undefined,
        }),
        to: "/docker/containers",
      });
    }
    setContainerEditMode(!containerEditMode);
  }, [containerEditMode, navigate]);

  const actions = (
    <>
      {checkUpdatesButton}
      <AppActionIconButton
        ariaLabel="Container auto-update settings"
        disabled={!watchtowerEnabled}
        icon="mdi:timer-cog-outline"
        iconSize={20}
        label={
          watchtowerEnabled
            ? "Container auto-update settings"
            : watchtowerReason
        }
        onClick={() => setAutoUpdateDialogOpen(true)}
      />
      <ViewModeToggle
        alternateMode="table"
        onViewModeChange={setContainerView}
        viewMode={containerView}
      />
      <AppTooltip title={containerEditMode ? "Lock layout" : "Edit layout"}>
        <AppIconButton
          aria-label={containerEditMode ? "Lock layout" : "Edit layout"}
          color={containerEditMode ? "primary" : "default"}
          onClick={handleToggleContainerEditMode}
          size="small"
        >
          <Icon height={20} icon="mdi:drag" width={20} />
        </AppIconButton>
      </AppTooltip>
    </>
  );

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <ContainerList
        checkingUpdates={isCheckingUpdates}
        containerAutoUpdate={containerAutoUpdate}
        editMode={containerEditMode}
        viewMode={containerView}
      />
      <ContainerAutoUpdateDialog
        autoUpdate={containerAutoUpdate}
        onClose={() => setAutoUpdateDialogOpen(false)}
        open={autoUpdateDialogOpen}
        watchtowerEnabled={watchtowerEnabled}
        watchtowerReason={watchtowerReason}
      />
    </>
  );
};

export default DockerContainersPage;
