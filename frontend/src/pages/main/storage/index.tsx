import { Add as AddIcon } from "@mui/icons-material";
import { Box, Button } from "@mui/material";
import React, { useState } from "react";

import DiskOverview from "./DiskOverview";
import LVMManagement from "./LVMManagement";
import NFSMounts from "./NFSMounts";

import { TabContainer } from "@/components/tabbar";

const StoragePage: React.FC = () => {
  const [createLVHandler, setCreateLVHandler] = useState<(() => void) | null>(
    null,
  );
  const [mountNFSHandler, setMountNFSHandler] = useState<(() => void) | null>(
    null,
  );

  return (
    <TabContainer
      tabs={[
        {
          value: "disks",
          label: "Disks",
          component: <DiskOverview />,
        },
        {
          value: "lvm",
          label: "LVM",
          component: (
            <LVMManagement
              onMountCreateHandler={(handler) =>
                setCreateLVHandler(() => handler)
              }
            />
          ),
          rightContent: createLVHandler ? (
            <Button
              variant="contained"
              size="small"
              onClick={createLVHandler}
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
                Create LV
              </Box>
              <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
            </Button>
          ) : undefined,
        },
        {
          value: "nfs",
          label: "NFS Mounts",
          component: (
            <NFSMounts
              onMountCreateHandler={(handler) =>
                setMountNFSHandler(() => handler)
              }
            />
          ),
          rightContent: mountNFSHandler ? (
            <Button
              variant="contained"
              size="small"
              onClick={mountNFSHandler}
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
                Mount NFS
              </Box>
              <AddIcon sx={{ display: { xs: "block", sm: "none" } }} />
            </Button>
          ) : undefined,
        },
      ]}
      defaultTab="disks"
      urlParam="storageTab"
    />
  );
};

export default StoragePage;
