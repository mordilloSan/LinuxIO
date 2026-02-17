import { Add as AddIcon } from "@mui/icons-material";
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Tooltip,
  Typography,
} from "@mui/material";
import React, { useState } from "react";

import ComposeStacksPage from "./ComposeStacksPage";
import ContainerList from "./ContainerList";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";
import VolumeList from "./VolumeList";

import { TabContainer } from "@/components/tabbar";
import useAuth from "@/hooks/useAuth";

const DockerPage: React.FC = () => {
  const { dockerAvailable, indexerAvailable } = useAuth();

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
    <TabContainer
      tabs={[
        {
          value: "containers",
          label: "Containers",
          component: <ContainerList />,
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
                      <Box sx={{ display: { xs: "block", sm: "none" } }}>↻</Box>
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
      defaultTab="containers"
      urlParam="dockerTab"
    />
  );
};

export default DockerPage;
