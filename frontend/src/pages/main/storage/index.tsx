import { Add as AddIcon } from "@mui/icons-material";
import { Button } from "@mui/material";
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
              startIcon={<AddIcon />}
            >
              Create LV
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
              startIcon={<AddIcon />}
            >
              Mount NFS
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
