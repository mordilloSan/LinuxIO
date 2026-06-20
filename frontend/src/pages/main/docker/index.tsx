import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import ComposeStacksPage from "./ComposeStacksPage";
import ContainerAutoUpdateDialog from "./ContainerAutoUpdateDialog";
import ContainerList from "./ContainerList";
import DockerDashboard from "./DockerDashboard";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";
import VolumeList from "./VolumeList";

import { jobSnapshotResult, linuxio } from "@/api";
import PruneDialog, { PruneOptions } from "@/components/docker/PruneDialog";
import { TabContainer } from "@/components/tabbar";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useViewMode } from "@/hooks/useViewMode";
import { useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

const DockerPage: React.FC = () => {
  const theme = useAppTheme();
  const [searchParams] = useSearchParams();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
  const { status: dockerStatus } = useCapability("dockerAvailable");
  const { isEnabled: watchtowerEnabled, reason: watchtowerReason } =
    useCapability("watchtowerAvailable");
  const queryClient = useQueryClient();
  const [pruneDialogOpen, setPruneDialogOpen] = useState(false);
  const [autoUpdateDialogOpen, setAutoUpdateDialogOpen] = useState(false);
  const activeDockerTab = searchParams.get("dockerTab") || "dashboard";
  const isDashboardTab = activeDockerTab === "dashboard";
  const { data: rawContainers } = linuxio.docker.list_containers.useQuery({
    enabled: isDashboardTab,
    refetchInterval: isDashboardTab ? 5000 : false,
  });
  const containers = useMemo(() => rawContainers ?? [], [rawContainers]);
  const stoppedContainers = useMemo(
    () => containers.filter((c) => c.State === "exited" || c.State === "dead"),
    [containers],
  );
  const runningContainers = useMemo(
    () => containers.filter((c) => c.State === "running"),
    [containers],
  );
  const invalidateDockerUpdateViews = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_containers.queryKey(),
    });
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_compose_projects.queryKey(),
    });
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_images.queryKey(),
    });
  }, [queryClient]);
  const { mutate: checkUpdates, isPending: isCheckingUpdates } =
    linuxio.docker.check_updates.useMutation({
      onSuccess: (data) => {
        const result =
          jobSnapshotResult<{
            checked?: number;
            updates?: number;
          }>(data) ?? {};
        const checked = result.checked ?? 0;
        const updates = result.updates ?? 0;
        toast.success(
          `Checked ${checked} container(s), found ${updates} update(s)`,
        );
        invalidateDockerUpdateViews();
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, "Failed to check updates")),
    });
  const { mutate: startAllStopped, isPending: isStartingAll } =
    linuxio.docker.start_all_stopped.useMutation({
      onSuccess: (data: any) => {
        const result = jobSnapshotResult<{ started: number }>(data);
        toast.success(`Started ${result.started} container(s)`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, "Failed to start containers")),
    });
  const { mutate: stopAllRunning, isPending: isStoppingAll } =
    linuxio.docker.stop_all_running.useMutation({
      onSuccess: (data: any) => {
        const result = jobSnapshotResult<{ stopped: number }>(data);
        toast.success(`Stopped ${result.stopped} container(s)`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, "Failed to stop containers")),
    });
  const { mutate: systemPrune, isPending: isPruning } =
    linuxio.docker.system_prune.useMutation({
      onSuccess: () => {
        toast.success("Docker prune completed");
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_images.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_volumes.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_networks.queryKey(),
        });
        setPruneDialogOpen(false);
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, "Prune failed")),
    });
  const [containerView, setContainerView] = useViewMode(
    "docker.containers",
    "card",
  );
  const [stacksView, setStacksView] = useViewMode("docker.stacks", "table");
  const [networksView, setNetworksView] = useViewMode(
    "docker.networks",
    "table",
  );
  const [volumesView, setVolumesView] = useViewMode("docker.volumes", "table");
  const [imagesView, setImagesView] = useViewMode("docker.images", "table");
  const [createStackHandler, setCreateStackHandler] = useState<
    (() => void) | null
  >(null);
  const [createNetworkHandler, setCreateNetworkHandler] = useState<
    (() => void) | null
  >(null);
  const [createVolumeHandler, setCreateVolumeHandler] = useState<
    (() => void) | null
  >(null);
  const [createImageHandler, setCreateImageHandler] = useState<
    (() => void) | null
  >(null);
  const [containerEditMode, setContainerEditMode] = useState(false);
  const handleMountCreateStackHandler = useCallback((handler: () => void) => {
    setCreateStackHandler(() => handler);
  }, []);
  const handleMountCreateNetworkHandler = useCallback((handler: () => void) => {
    setCreateNetworkHandler(() => handler);
  }, []);
  const handleMountCreateVolumeHandler = useCallback((handler: () => void) => {
    setCreateVolumeHandler(() => handler);
  }, []);
  const handleMountCreateImageHandler = useCallback((handler: () => void) => {
    setCreateImageHandler(() => handler);
  }, []);
  const renderCheckUpdatesButton = () => {
    const button = (
      <AppButton
        disabled={isCheckingUpdates || !watchtowerEnabled}
        onClick={() => checkUpdates()}
        size="small"
        startIcon={
          isCheckingUpdates ? (
            <AppCircularProgress color="inherit" size={18} />
          ) : (
            <Icon height={20} icon="mdi:update" width={20} />
          )
        }
        variant="outlined"
      >
        Check Updates
      </AppButton>
    );
    if (watchtowerEnabled) {
      return button;
    }
    return (
      <AppTooltip title={watchtowerReason}>
        <span>{button}</span>
      </AppTooltip>
    );
  };
  const renderAutoUpdateSettingsButton = () => {
    const button = (
      <AppIconButton
        aria-label="Container auto-update settings"
        disabled={!watchtowerEnabled}
        onClick={() => setAutoUpdateDialogOpen(true)}
        size="small"
      >
        <Icon height={20} icon="mdi:timer-cog-outline" width={20} />
      </AppIconButton>
    );
    return (
      <AppTooltip
        title={
          watchtowerEnabled
            ? "Container auto-update settings"
            : watchtowerReason
        }
      >
        <span>{button}</span>
      </AppTooltip>
    );
  };
  if (dockerStatus === "unknown") {
    return (
      <div
        style={{
          padding: theme.spacing(3),
        }}
      >
        <AppAlert severity="info">
          <AppAlertTitle>Checking Docker</AppAlertTitle>
          <AppTypography variant="body2">
            Verifying Docker daemon access...
          </AppTypography>
        </AppAlert>
      </div>
    );
  }

  // Show error if Docker is not available
  if (dockerStatus === "unavailable") {
    return (
      <div
        style={{
          padding: theme.spacing(3),
        }}
      >
        <AppAlert severity="warning">
          <AppAlertTitle>Docker Not Available</AppAlertTitle>
          <AppTypography
            style={{
              marginBottom: 8,
            }}
            variant="body2"
          >
            Docker daemon is not accessible
          </AppTypography>
          <AppTypography component="div" variant="body2">
            <strong>Common causes:</strong>
            <ul
              style={{
                marginTop: theme.spacing(1),
                marginBottom: 0,
              }}
            >
              <li>Docker is not installed on this system</li>
              <li>
                Docker service is not running (try: sudo systemctl start docker)
              </li>
              <li>
                You don&apos;t have permission to access the Docker socket
                <br />
                (try: sudo usermod -aG docker $USER, then log out and back in)
              </li>
              <li>
                Docker socket path is not set correctly (check DOCKER_HOST
                environment variable)
              </li>
            </ul>
          </AppTypography>
        </AppAlert>
      </div>
    );
  }
  return (
    <>
      <TabContainer
        defaultTab="dashboard"
        tabs={[
          {
            value: "dashboard",
            label: "Dashboard",
            component: <DockerDashboard />,
            rightContent: (
              <>
                {renderCheckUpdatesButton()}
                {renderAutoUpdateSettingsButton()}
                <AppButton
                  disabled={isStartingAll || stoppedContainers.length === 0}
                  onClick={() => startAllStopped()}
                  size="small"
                  startIcon={<Icon height={20} icon="mdi:play" width={20} />}
                  variant="outlined"
                >
                  Start All
                </AppButton>
                <AppButton
                  color="warning"
                  disabled={isStoppingAll || runningContainers.length === 0}
                  onClick={() => stopAllRunning()}
                  size="small"
                  startIcon={<Icon height={20} icon="mdi:stop" width={20} />}
                  variant="outlined"
                >
                  Stop All
                </AppButton>
                <AppButton
                  color="error"
                  disabled={isPruning}
                  onClick={() => setPruneDialogOpen(true)}
                  size="small"
                  startIcon={<Icon height={20} icon="mdi:broom" width={20} />}
                  variant="outlined"
                >
                  Prune All
                </AppButton>
              </>
            ),
          },
          {
            value: "containers",
            label: "Containers",
            component: (
              <ContainerList
                checkingUpdates={isCheckingUpdates}
                editMode={containerEditMode}
                viewMode={containerView}
              />
            ),
            rightContent: (
              <>
                {renderCheckUpdatesButton()}
                {renderAutoUpdateSettingsButton()}
                <AppTooltip
                  title={
                    containerView === "card"
                      ? "Switch to table view"
                      : "Switch to card view"
                  }
                >
                  <AppIconButton
                    onClick={() =>
                      setContainerView(
                        containerView === "card" ? "table" : "card",
                      )
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
                <AppTooltip
                  title={containerEditMode ? "Lock layout" : "Edit layout"}
                >
                  <AppIconButton
                    color={containerEditMode ? "primary" : "default"}
                    onClick={() => setContainerEditMode((prev) => !prev)}
                    size="small"
                  >
                    <Icon height={20} icon="mdi:drag" width={20} />
                  </AppIconButton>
                </AppTooltip>
              </>
            ),
          },
          {
            value: "compose",
            label: "Stacks",
            component: (
              <ComposeStacksPage
                onMountCreateHandler={handleMountCreateStackHandler}
                viewMode={stacksView}
              />
            ),
            rightContent: (
              <>
                {renderCheckUpdatesButton()}
                {renderAutoUpdateSettingsButton()}
                <AppTooltip
                  title={
                    stacksView === "table"
                      ? "Switch to card view"
                      : "Switch to table view"
                  }
                >
                  <AppIconButton
                    onClick={() =>
                      setStacksView(stacksView === "table" ? "card" : "table")
                    }
                    size="small"
                  >
                    {stacksView === "table" ? (
                      <Icon height={20} icon="mdi:card-multiple" width={20} />
                    ) : (
                      <Icon height={20} icon="mdi:table" width={20} />
                    )}
                  </AppIconButton>
                </AppTooltip>
                {createStackHandler && (
                  <AppButton
                    onClick={createStackHandler}
                    size="small"
                    startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
                    variant="contained"
                  >
                    Create Stack
                  </AppButton>
                )}
              </>
            ),
          },
          {
            value: "networks",
            label: "Networks",
            component: (
              <DockerNetworksTable
                onMountCreateHandler={handleMountCreateNetworkHandler}
                viewMode={networksView}
              />
            ),
            rightContent: (
              <>
                <AppTooltip
                  title={
                    networksView === "table"
                      ? "Switch to card view"
                      : "Switch to table view"
                  }
                >
                  <AppIconButton
                    onClick={() =>
                      setNetworksView(
                        networksView === "table" ? "card" : "table",
                      )
                    }
                    size="small"
                  >
                    {networksView === "table" ? (
                      <Icon height={20} icon="mdi:card-multiple" width={20} />
                    ) : (
                      <Icon height={20} icon="mdi:table" width={20} />
                    )}
                  </AppIconButton>
                </AppTooltip>
                {createNetworkHandler && (
                  <AppButton
                    onClick={createNetworkHandler}
                    size="small"
                    startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
                    variant="contained"
                  >
                    Add Network
                  </AppButton>
                )}
              </>
            ),
          },
          {
            value: "volumes",
            label: "Volumes",
            component: (
              <VolumeList
                onMountCreateHandler={handleMountCreateVolumeHandler}
                viewMode={volumesView}
              />
            ),
            rightContent: (
              <>
                <AppTooltip
                  title={
                    volumesView === "table"
                      ? "Switch to card view"
                      : "Switch to table view"
                  }
                >
                  <AppIconButton
                    onClick={() =>
                      setVolumesView(volumesView === "table" ? "card" : "table")
                    }
                    size="small"
                  >
                    {volumesView === "table" ? (
                      <Icon height={20} icon="mdi:card-multiple" width={20} />
                    ) : (
                      <Icon height={20} icon="mdi:table" width={20} />
                    )}
                  </AppIconButton>
                </AppTooltip>
                {createVolumeHandler && (
                  <AppButton
                    onClick={createVolumeHandler}
                    size="small"
                    startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
                    variant="contained"
                  >
                    Add Volume
                  </AppButton>
                )}
              </>
            ),
          },
          {
            value: "images",
            label: "Images",
            component: (
              <ImageList
                onMountCreateHandler={handleMountCreateImageHandler}
                viewMode={imagesView}
              />
            ),
            rightContent: (
              <>
                {renderCheckUpdatesButton()}
                {renderAutoUpdateSettingsButton()}
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
            ),
          },
        ]}
        urlParam="dockerTab"
      />
      <PruneDialog
        isLoading={isPruning}
        onClose={() => !isPruning && setPruneDialogOpen(false)}
        onConfirm={(opts: PruneOptions) => systemPrune(opts)}
        open={pruneDialogOpen}
      />
      <ContainerAutoUpdateDialog
        onClose={() => setAutoUpdateDialogOpen(false)}
        open={autoUpdateDialogOpen}
        watchtowerEnabled={watchtowerEnabled}
        watchtowerReason={watchtowerReason}
      />
    </>
  );
};
export default DockerPage;
