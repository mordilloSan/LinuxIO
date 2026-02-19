import {
  Add as AddIcon,
  CleaningServices as CleaningServicesIcon,
  DragIndicator,
  PlayArrow as PlayArrowIcon,
  Stop as StopIcon,
} from "@mui/icons-material";
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  IconButton,
  Tooltip,
  Typography,
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
import useAuth from "@/hooks/useAuth";
import { getMutationErrorMessage } from "@/utils/mutations";

const DockerPage: React.FC = () => {
  const { dockerAvailable, indexerAvailable } = useAuth();
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

  if (dockerAvailable === null) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="info">
          <AlertTitle>Checking Docker</AlertTitle>
          <Typography variant="body2">
            Verifying Docker daemon access...
          </Typography>
        </Alert>
      </Box>
    );
  }

  // Show error if Docker is not available
  if (dockerAvailable === false) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="warning">
          <AlertTitle>Docker Not Available</AlertTitle>
          <Typography variant="body2" sx={{ mb: 2 }}>
            Docker daemon is not accessible
          </Typography>
          <Typography variant="body2" component="div">
            <strong>Common causes:</strong>
            <Box component="ul" sx={{ mt: 1, mb: 0 }}>
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
            </Box>
          </Typography>
        </Alert>
      </Box>
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
                  sx={{
                    minWidth: { xs: "40px", sm: "auto" },
                    px: { xs: 1, sm: 2 },
                    "& .MuiButton-startIcon": {
                      margin: { xs: 0, sm: "0 8px 0 -4px" },
                    },
                  }}
                >
                  <Box sx={{ display: { xs: "none", sm: "block" } }}>
                    Start All
                  </Box>
                </Button>
                <Button
                  size="small"
                  variant="outlined"
                  color="warning"
                  startIcon={<StopIcon />}
                  disabled={isStoppingAll || runningContainers.length === 0}
                  onClick={() => stopAllRunning([])}
                  sx={{
                    minWidth: { xs: "40px", sm: "auto" },
                    px: { xs: 1, sm: 2 },
                    "& .MuiButton-startIcon": {
                      margin: { xs: 0, sm: "0 8px 0 -4px" },
                    },
                  }}
                >
                  <Box sx={{ display: { xs: "none", sm: "block" } }}>
                    Stop All
                  </Box>
                </Button>
                <Button
                  size="small"
                  variant="outlined"
                  color="error"
                  startIcon={<CleaningServicesIcon />}
                  disabled={isPruning}
                  onClick={() => setPruneDialogOpen(true)}
                  sx={{
                    minWidth: { xs: "40px", sm: "auto" },
                    px: { xs: 1, sm: 2 },
                    "& .MuiButton-startIcon": {
                      margin: { xs: 0, sm: "0 8px 0 -4px" },
                    },
                  }}
                >
                  <Box sx={{ display: { xs: "none", sm: "block" } }}>
                    Prune System
                  </Box>
                </Button>
              </>
            ),
          },
          {
            value: "containers",
            label: "Containers",
            component: <ContainerList editMode={containerEditMode} />,
            rightContent: (
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
              />
            ),
            rightContent: (
              <>
                {reindexStackHandler && (
                  <Tooltip
                    title={
                      indexerAvailable === false
                        ? "Indexer service is not available. Start linuxio-indexer.service to enable scanning."
                        : "Scan Docker folder for compose stacks"
                    }
                    arrow
                  >
                    <span>
                      <Button
                        variant="outlined"
                        size="small"
                        onClick={reindexStackHandler}
                        disabled={indexerAvailable === false}
                        sx={{
                          minWidth: { xs: "40px", sm: "auto" },
                          px: { xs: 1, sm: 2 },
                          mr: 1,
                        }}
                      >
                        <Box sx={{ display: { xs: "none", sm: "block" } }}>
                          Scan
                        </Box>
                        <Box sx={{ display: { xs: "block", sm: "none" } }}>
                          ↻
                        </Box>
                      </Button>
                    </span>
                  </Tooltip>
                )}
                {createStackHandler && (
                  <Button
                    variant="contained"
                    size="small"
                    onClick={createStackHandler}
                    sx={{
                      minWidth: { xs: "40px", sm: "auto" },
                      px: { xs: 1, sm: 2 },
                      "& .MuiButton-startIcon": {
                        display: { xs: "none", sm: "flex" },
                        margin: { xs: 0, sm: "0 8px 0 -4px" },
                      },
                    }}
                    startIcon={<AddIcon />}
                  >
                    <Box
                      sx={{
                        display: { xs: "none", sm: "flex" },
                        alignItems: "center",
                      }}
                    >
                      Create Stack
                    </Box>
                    <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
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
              />
            ),
            rightContent: createNetworkHandler ? (
              <Button
                variant="contained"
                size="small"
                onClick={createNetworkHandler}
                sx={{
                  minWidth: { xs: "40px", sm: "auto" },
                  px: { xs: 1, sm: 2 },
                  "& .MuiButton-startIcon": {
                    display: { xs: "none", sm: "flex" },
                    margin: { xs: 0, sm: "0 8px 0 -4px" },
                  },
                }}
                startIcon={<AddIcon />}
              >
                <Box
                  sx={{
                    display: { xs: "none", sm: "flex" },
                    alignItems: "center",
                  }}
                >
                  Add Network
                </Box>
                <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
              </Button>
            ) : undefined,
          },
          {
            value: "volumes",
            label: "Volumes",
            component: (
              <VolumeList
                onMountCreateHandler={(handler) =>
                  setCreateVolumeHandler(() => handler)
                }
              />
            ),
            rightContent: createVolumeHandler ? (
              <Button
                variant="contained"
                size="small"
                onClick={createVolumeHandler}
                sx={{
                  minWidth: { xs: "40px", sm: "auto" },
                  px: { xs: 1, sm: 2 },
                  "& .MuiButton-startIcon": {
                    display: { xs: "none", sm: "flex" },
                    margin: { xs: 0, sm: "0 8px 0 -4px" },
                  },
                }}
                startIcon={<AddIcon />}
              >
                <Box
                  sx={{
                    display: { xs: "none", sm: "flex" },
                    alignItems: "center",
                  }}
                >
                  Add Volume
                </Box>
                <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
              </Button>
            ) : undefined,
          },
          {
            value: "images",
            label: "Images",
            component: (
              <ImageList
                onMountCreateHandler={(handler) =>
                  setCreateImageHandler(() => handler)
                }
              />
            ),
            rightContent: createImageHandler ? (
              <Button
                variant="contained"
                size="small"
                onClick={createImageHandler}
                sx={{
                  minWidth: { xs: "40px", sm: "auto" },
                  px: { xs: 1, sm: 2 },
                  "& .MuiButton-startIcon": {
                    display: { xs: "none", sm: "flex" },
                    margin: { xs: 0, sm: "0 8px 0 -4px" },
                  },
                }}
                startIcon={<AddIcon />}
              >
                <Box
                  sx={{
                    display: { xs: "none", sm: "flex" },
                    alignItems: "center",
                  }}
                >
                  Add Image
                </Box>
                <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
              </Button>
            ) : undefined,
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
