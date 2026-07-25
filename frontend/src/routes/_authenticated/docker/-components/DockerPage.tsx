import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useMemo, useRef, useState } from "react";

import { linuxio, type ContainerInfo } from "@/api";
import PruneDialog, { PruneOptions } from "@/components/docker/PruneDialog";
import { TabContainer } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
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

import ComposeStacksPage from "./ComposeStacksPage";
import ContainerAutoUpdateDialog from "./ContainerAutoUpdateDialog";
import ContainerList from "./ContainerList";
import DockerDashboard from "./DockerDashboard";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";
import { useContainerAutoUpdateState } from "./useContainerAutoUpdateState";
import VolumeList from "./VolumeList";

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
const dockerRouteApi = getRouteApi("/_authenticated/docker");

const getContainerName = (container: ContainerInfo) =>
  container.Names?.[0]?.replace("/", "") || "Unnamed";

const DockerPage = () => {
  const theme = useAppTheme();
  const navigate = dockerRouteApi.useNavigate();
  const searchParams = dockerRouteApi.useSearch();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const { status: dockerStatus } = useCapability("dockerAvailable");
  const { isEnabled: watchtowerEnabled, reason: watchtowerReason } =
    useCapability("watchtowerAvailable");
  const [pruneDialogOpen, setPruneDialogOpen] = useState(false);
  const [autoUpdateDialogOpen, setAutoUpdateDialogOpen] = useState(false);
  // Single writer for auto-update state, shared by the container list's
  // toggles and the settings dialog so their saves cannot race.
  const containerAutoUpdate = useContainerAutoUpdateState();
  const stopAllInFlightRef = useRef(false);
  const [stoppingContainerIds, setStoppingContainerIds] = useState<Set<string>>(
    () => new Set(),
  );
  const activeDockerTab =
    searchParams.dockerTab === "containers" ||
    searchParams.dockerTab === "compose" ||
    searchParams.dockerTab === "networks" ||
    searchParams.dockerTab === "volumes" ||
    searchParams.dockerTab === "images"
      ? searchParams.dockerTab
      : "dashboard";
  const isDashboardTab = activeDockerTab === "dashboard";
  const { data: rawContainers, isLoading: dashboardContainersLoading } =
    useQuery(
      linuxio.docker.list_containers.queryOptions({
        enabled: isDashboardTab,
        refetchInterval: isDashboardTab ? 5000 : false,
      }),
    );
  const containers = useMemo(() => rawContainers ?? [], [rawContainers]);
  const stoppedContainers = useMemo(
    () => containers.filter((c) => c.State === "exited" || c.State === "dead"),
    [containers],
  );
  const runningContainers = useMemo(
    () => containers.filter((c) => c.State === "running"),
    [containers],
  );
  const { mutate: checkUpdates, isPending: isCheckingUpdates } =
    linuxio.docker.check_updates.useJobAction({
      success: (result) => {
        const checked = result?.checked ?? 0;
        const updates = result?.updates ?? 0;
        toast.success(
          `Checked ${checked} container(s), found ${updates} update(s)`,
        );
      },
      error: "Failed to check updates",
      toast: DOCKER_TOAST_META,
    });
  const { mutate: startAllStopped, isPending: isStartingAll } =
    linuxio.docker.start_all_stopped.useJobAction({
      success: (result) => {
        toast.success(`Started ${result.started} container(s)`);
      },
      error: "Failed to start containers",
      toast: DOCKER_TOAST_META,
    });
  // Configless: this is a batch flow — the caller owns aggregation and toasts.
  const { mutateAsync: stopContainer } =
    linuxio.docker.stop_container.useJobAction();
  const isStoppingAll = stoppingContainerIds.size > 0;
  const handleStopAllRunning = async () => {
    if (stopAllInFlightRef.current || runningContainers.length === 0) return;

    const targets = [...runningContainers];
    stopAllInFlightRef.current = true;
    setStoppingContainerIds(new Set(targets.map((container) => container.Id)));

    const failures: string[] = [];
    // No try/finally (the React Compiler oxc port cannot lower finalizers):
    // each stop swallows its own error, so the resets below always run.
    for (const container of targets) {
      try {
        await stopContainer({ containerId: container.Id });
      } catch {
        failures.push(getContainerName(container));
      }
      setStoppingContainerIds((previous: Set<string>) => {
        const next = new Set(previous);
        next.delete(container.Id);
        return next;
      });
    }
    stopAllInFlightRef.current = false;
    setStoppingContainerIds(new Set());
    if (failures.length > 0) {
      toast.error(
        `Failed to stop ${failures.length} of ${targets.length} container${targets.length === 1 ? "" : "s"}`,
      );
    } else {
      toast.success(`Stopped ${targets.length} container(s)`);
    }
  };
  const { mutate: systemPrune, isPending: isPruning } =
    linuxio.docker.system_prune.useJobAction({
      success: () => {
        toast.success("Docker prune completed");
        setPruneDialogOpen(false);
      },
      error: "Prune failed",
      toast: DOCKER_TOAST_META,
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
  const handleToggleContainerEditMode = useCallback(() => {
    if (!containerEditMode) {
      navigate({
        to: "/docker",
        search: (previous) => ({
          ...previous,
          container: undefined,
        }),
      });
    }
    setContainerEditMode(!containerEditMode);
  }, [containerEditMode, navigate]);
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
  const renderAutoUpdateSettingsButton = () => (
    <AppActionIconButton
      ariaLabel="Container auto-update settings"
      disabled={!watchtowerEnabled}
      icon="mdi:timer-cog-outline"
      iconSize={20}
      label={
        watchtowerEnabled ? "Container auto-update settings" : watchtowerReason
      }
      onClick={() => setAutoUpdateDialogOpen(true)}
    />
  );
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
        activeTab={activeDockerTab}
        onTabChange={(dockerTab) =>
          navigate({
            to: "/docker",
            search: (previous) => ({ ...previous, dockerTab }),
          })
        }
        tabs={[
          {
            value: "dashboard",
            label: "Dashboard",
            component: (
              <DockerDashboard stoppingContainerIds={stoppingContainerIds} />
            ),
            rightContent: (
              <>
                {renderCheckUpdatesButton()}
                {renderAutoUpdateSettingsButton()}
                <AppButton
                  disabled={
                    dashboardContainersLoading ||
                    isStartingAll ||
                    stoppedContainers.length === 0
                  }
                  onClick={() => startAllStopped()}
                  size="small"
                  startIcon={
                    isStartingAll ? (
                      <AppCircularProgress color="inherit" size={18} />
                    ) : (
                      <Icon height={20} icon="mdi:play" width={20} />
                    )
                  }
                  variant="outlined"
                >
                  Start All
                </AppButton>
                <AppButton
                  color="warning"
                  disabled={
                    dashboardContainersLoading ||
                    isStoppingAll ||
                    runningContainers.length === 0
                  }
                  onClick={() => void handleStopAllRunning()}
                  size="small"
                  startIcon={
                    isStoppingAll ? (
                      <AppCircularProgress color="inherit" size={18} />
                    ) : (
                      <Icon height={20} icon="mdi:stop" width={20} />
                    )
                  }
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
                containerAutoUpdate={containerAutoUpdate}
                editMode={containerEditMode}
                stoppingContainerIds={stoppingContainerIds}
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
                    onClick={handleToggleContainerEditMode}
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
      />
      <PruneDialog
        isLoading={isPruning}
        onClose={() => !isPruning && setPruneDialogOpen(false)}
        onConfirm={(opts: PruneOptions) => systemPrune(opts)}
        open={pruneDialogOpen}
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
export default DockerPage;
