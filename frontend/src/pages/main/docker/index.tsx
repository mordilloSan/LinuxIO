import {
  Add as AddIcon,
  CleaningServices as CleaningServicesIcon,
  DragIndicator,
  GridView as GridViewIcon,
  PlayArrow as PlayArrowIcon,
  Stop as StopIcon,
  TableRows as TableRowsIcon,
} from "@mui/icons-material";
import {
  Alert,
  AlertTitle,
  Button,
  IconButton,
  Tooltip,
  Typography,
  useTheme,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useMemo, useState } from "react";
import { toast } from "sonner";

import ComposeStacksPage from "./ComposeStacksPage";
import ContainerList from "./ContainerList";
import DockerDashboard from "./DockerDashboard";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";
import VolumeList from "./VolumeList";

import { linuxio } from "@/api";
import PruneDialog, { PruneOptions } from "@/components/docker/PruneDialog";
import { TabContainer } from "@/components/tabbar";
import { useCapability } from "@/hooks/useCapabilities";
import { useViewMode } from "@/hooks/useViewMode";
import { getMutationErrorMessage } from "@/utils/mutations";

const DockerPage: React.FC = () => {
  const theme = useTheme();
  const { status: dockerStatus } = useCapability("dockerAvailable");
  const { isEnabled: indexerEnabled, reason: indexerReason } =
    useCapability("indexerAvailable");
  const queryClient = useQueryClient();
  const [pruneDialogOpen, setPruneDialogOpen] = useState(false);

  const { data: containers = [] } = linuxio.docker.list_containers.useQuery({
    refetchInterval: 5000,
  });

  const stoppedContainers = useMemo(
    () => containers.filter((c) => c.State === "exited" || c.State === "dead"),
    [containers],
  );
  const runningContainers = useMemo(
    () => containers.filter((c) => c.State === "running"),
    [containers],
  );

  const { mutate: startAllStopped, isPending: isStartingAll } =
    linuxio.docker.start_all_stopped.useMutation({
      onSuccess: (data: any) => {
        toast.success(`Started ${data.started} container(s)`);
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
        toast.success(`Stopped ${data.stopped} container(s)`);
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
  const [reindexStackHandler, setReindexStackHandler] = useState<
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

  if (dockerStatus === "unknown") {
    return (
      <div style={{ padding: theme.spacing(3) }}>
        <Alert severity="info">
          <AlertTitle>Checking Docker</AlertTitle>
          <Typography variant="body2">
            Verifying Docker daemon access...
          </Typography>
        </Alert>
      </div>
    );
  }

  // Show error if Docker is not available
  if (dockerStatus === "unavailable") {
    return (
      <div style={{ padding: theme.spacing(3) }}>
        <Alert severity="warning">
          <AlertTitle>Docker Not Available</AlertTitle>
          <Typography variant="body2" sx={{ mb: 2 }}>
            Docker daemon is not accessible
          </Typography>
          <Typography variant="body2" component="div">
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
          </Typography>
        </Alert>
      </div>
    );
  }

  return (
    <>
      <TabContainer
        tabs={[
          {
            value: "dashboard",
            label: "Dashboard",
            component: <DockerDashboard />,
            rightContent: (
              <>
                <Button
                  size="small"
                  variant="outlined"
                  startIcon={<PlayArrowIcon />}
                  disabled={isStartingAll || stoppedContainers.length === 0}
                  onClick={() => startAllStopped([])}
                >
                  Start All
                </Button>
                <Button
                  size="small"
                  variant="outlined"
                  color="warning"
                  startIcon={<StopIcon />}
                  disabled={isStoppingAll || runningContainers.length === 0}
                  onClick={() => stopAllRunning([])}
                >
                  Stop All
                </Button>
                <Button
                  size="small"
                  variant="outlined"
                  color="error"
                  startIcon={<CleaningServicesIcon />}
                  disabled={isPruning}
                  onClick={() => setPruneDialogOpen(true)}
                >
                  Prune All
                </Button>
              </>
            ),
          },
          {
            value: "containers",
            label: "Containers",
            component: (
              <ContainerList
                editMode={containerEditMode}
                viewMode={containerView}
              />
            ),
            rightContent: (
              <>
                <Tooltip
                  title={
                    containerView === "card"
                      ? "Switch to table view"
                      : "Switch to card view"
                  }
                >
                  <IconButton
                    size="small"
                    onClick={() =>
                      setContainerView(
                        containerView === "card" ? "table" : "card",
                      )
                    }
                  >
                    {containerView === "card" ? (
                      <TableRowsIcon fontSize="small" />
                    ) : (
                      <GridViewIcon fontSize="small" />
                    )}
                  </IconButton>
                </Tooltip>
                <Tooltip
                  title={containerEditMode ? "Lock layout" : "Edit layout"}
                >
                  <IconButton
                    onClick={() => setContainerEditMode((prev) => !prev)}
                    color={containerEditMode ? "primary" : "default"}
                    size="small"
                  >
                    <DragIndicator />
                  </IconButton>
                </Tooltip>
              </>
            ),
          },
          {
            value: "compose",
            label: "Stacks",
            component: (
              <ComposeStacksPage
                onMountCreateHandler={(handler) =>
                  setCreateStackHandler(() => handler)
                }
                onMountIndexerHandler={(handler) =>
                  setReindexStackHandler(() => handler)
                }
                viewMode={stacksView}
              />
            ),
            rightContent: (
              <>
                <Tooltip
                  title={
                    stacksView === "table"
                      ? "Switch to card view"
                      : "Switch to table view"
                  }
                >
                  <IconButton
                    size="small"
                    onClick={() =>
                      setStacksView(stacksView === "table" ? "card" : "table")
                    }
                  >
                    {stacksView === "table" ? (
                      <GridViewIcon fontSize="small" />
                    ) : (
                      <TableRowsIcon fontSize="small" />
                    )}
                  </IconButton>
                </Tooltip>
                {reindexStackHandler && (
                  <Tooltip
                    title={
                      !indexerEnabled
                        ? indexerReason
                        : "Scan Docker folder for compose stacks"
                    }
                    arrow
                  >
                    <span>
                      <Button
                        variant="outlined"
                        size="small"
                        onClick={reindexStackHandler}
                        disabled={!indexerEnabled}
                        sx={{ mr: 1 }}
                      >
                        Scan
                      </Button>
                    </span>
                  </Tooltip>
                )}
                {createStackHandler && (
                  <Button
                    variant="contained"
                    size="small"
                    onClick={createStackHandler}
                    startIcon={<AddIcon />}
                  >
                    Create Stack
                  </Button>
                )}
              </>
            ),
          },
          {
            value: "networks",
            label: "Networks",
            component: (
              <DockerNetworksTable
                onMountCreateHandler={(handler) =>
                  setCreateNetworkHandler(() => handler)
                }
                viewMode={networksView}
              />
            ),
            rightContent: (
              <>
                <Tooltip
                  title={
                    networksView === "table"
                      ? "Switch to card view"
                      : "Switch to table view"
                  }
                >
                  <IconButton
                    size="small"
                    onClick={() =>
                      setNetworksView(
                        networksView === "table" ? "card" : "table",
                      )
                    }
                  >
                    {networksView === "table" ? (
                      <GridViewIcon fontSize="small" />
                    ) : (
                      <TableRowsIcon fontSize="small" />
                    )}
                  </IconButton>
                </Tooltip>
                {createNetworkHandler && (
                  <Button
                    variant="contained"
                    size="small"
                    onClick={createNetworkHandler}
                    startIcon={<AddIcon />}
                  >
                    Add Network
                  </Button>
                )}
              </>
            ),
          },
          {
            value: "volumes",
            label: "Volumes",
            component: (
              <VolumeList
                onMountCreateHandler={(handler) =>
                  setCreateVolumeHandler(() => handler)
                }
                viewMode={volumesView}
              />
            ),
            rightContent: (
              <>
                <Tooltip
                  title={
                    volumesView === "table"
                      ? "Switch to card view"
                      : "Switch to table view"
                  }
                >
                  <IconButton
                    size="small"
                    onClick={() =>
                      setVolumesView(volumesView === "table" ? "card" : "table")
                    }
                  >
                    {volumesView === "table" ? (
                      <GridViewIcon fontSize="small" />
                    ) : (
                      <TableRowsIcon fontSize="small" />
                    )}
                  </IconButton>
                </Tooltip>
                {createVolumeHandler && (
                  <Button
                    variant="contained"
                    size="small"
                    onClick={createVolumeHandler}
                    startIcon={<AddIcon />}
                  >
                    Add Volume
                  </Button>
                )}
              </>
            ),
          },
          {
            value: "images",
            label: "Images",
            component: (
              <ImageList
                onMountCreateHandler={(handler) =>
                  setCreateImageHandler(() => handler)
                }
                viewMode={imagesView}
              />
            ),
            rightContent: (
              <>
                <Tooltip
                  title={
                    imagesView === "table"
                      ? "Switch to card view"
                      : "Switch to table view"
                  }
                >
                  <IconButton
                    size="small"
                    onClick={() =>
                      setImagesView(imagesView === "table" ? "card" : "table")
                    }
                  >
                    {imagesView === "table" ? (
                      <GridViewIcon fontSize="small" />
                    ) : (
                      <TableRowsIcon fontSize="small" />
                    )}
                  </IconButton>
                </Tooltip>
                {createImageHandler && (
                  <Button
                    variant="contained"
                    size="small"
                    onClick={createImageHandler}
                    startIcon={<AddIcon />}
                  >
                    Add Image
                  </Button>
                )}
              </>
            ),
          },
        ]}
        defaultTab="dashboard"
        urlParam="dockerTab"
      />
      <PruneDialog
        open={pruneDialogOpen}
        onClose={() => !isPruning && setPruneDialogOpen(false)}
        onConfirm={(opts: PruneOptions) => systemPrune([JSON.stringify(opts)])}
        isLoading={isPruning}
      />
    </>
  );
};

export default DockerPage;
