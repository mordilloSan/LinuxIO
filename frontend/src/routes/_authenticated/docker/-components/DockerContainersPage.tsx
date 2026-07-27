import { Icon } from "@iconify/react";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useState } from "react";

import { RoutedTabContainer } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useCapability } from "@/hooks/useCapabilities";
import { useViewMode } from "@/hooks/useViewMode";

import ContainerAutoUpdateDialog from "./ContainerAutoUpdateDialog";
import ContainerList from "./ContainerList";
import { DOCKER_TABS } from "./dockerTabs";
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
      <AppTooltip
        title={
          containerView === "card"
            ? "Switch to table view"
            : "Switch to card view"
        }
      >
        <AppIconButton
          onClick={() =>
            setContainerView(containerView === "card" ? "table" : "card")
          }
          size="small"
        >
          {containerView === "card" ? (
            <Icon height={20} icon="mdi:table" width={20} />
          ) : (
            <Icon height={20} icon="mdi:card-multiple" width={20} />
          )}
        </AppIconButton>
      </AppTooltip>
      <AppTooltip title={containerEditMode ? "Lock layout" : "Edit layout"}>
        <AppIconButton
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
      <RoutedTabContainer rightContent={actions} tabs={DOCKER_TABS}>
        <ContainerList
          checkingUpdates={isCheckingUpdates}
          containerAutoUpdate={containerAutoUpdate}
          editMode={containerEditMode}
          viewMode={containerView}
        />
      </RoutedTabContainer>
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
