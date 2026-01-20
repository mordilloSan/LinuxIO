import { Add as AddIcon } from "@mui/icons-material";
import { Box, Button } from "@mui/material";
import React, { useState } from "react";

import ComposeStacksPage from "./ComposeStacksPage";
import ContainerList from "./ContainerList";
import ImageList from "./ImageList";
import DockerNetworksTable from "./NetworkList";
import VolumeList from "./VolumeList";

import { TabContainer } from "@/components/tabbar";

const DockerPage: React.FC = () => {
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
              onMountReindexHandler={(handler) =>
                setReindexStackHandler(() => handler)
              }
            />
          ),
          rightContent: (
            <>
              {reindexStackHandler && (
                <Button
                  variant="outlined"
                  size="small"
                  onClick={reindexStackHandler}
                  sx={{
                    minWidth: { xs: "40px", sm: "auto" },
                    px: { xs: 1, sm: 2 },
                    mr: 1,
                  }}
                >
                  <Box sx={{ display: { xs: "none", sm: "block" } }}>
                    Reindex
                  </Box>
                  <Box sx={{ display: { xs: "block", sm: "none" } }}>↻</Box>
                </Button>
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
