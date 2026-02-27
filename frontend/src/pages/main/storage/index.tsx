import {
  Add as AddIcon,
  GridView as GridViewIcon,
  TableRows as TableRowsIcon,
} from "@mui/icons-material";
import { Button, IconButton, Tooltip } from "@mui/material";
import React, { useState } from "react";

import DiskOverview from "./DiskOverview";
import LVMManagement from "./LVMManagement";
import NFSMounts from "./NFSMounts";

import { TabContainer } from "@/components/tabbar";
import { useViewMode } from "@/hooks/useViewMode";

const StoragePage: React.FC = () => {
  const [createLVHandler, setCreateLVHandler] = useState<(() => void) | null>(
    null,
  );
  const [mountNFSHandler, setMountNFSHandler] = useState<(() => void) | null>(
    null,
  );
  const [nfsView, setNfsView] = useViewMode("storage.nfs", "table");

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
              viewMode={nfsView}
            />
          ),
          rightContent: (
            <>
              <Tooltip
                title={
                  nfsView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <IconButton
                  size="small"
                  onClick={() =>
                    setNfsView(nfsView === "table" ? "card" : "table")
                  }
                >
                  {nfsView === "table" ? (
                    <GridViewIcon fontSize="small" />
                  ) : (
                    <TableRowsIcon fontSize="small" />
                  )}
                </IconButton>
              </Tooltip>
              {mountNFSHandler && (
                <Button
                  variant="contained"
                  size="small"
                  onClick={mountNFSHandler}
                  startIcon={<AddIcon />}
                >
                  Mount NFS
                </Button>
              )}
            </>
          ),
        },
      ]}
      defaultTab="disks"
      urlParam="storageTab"
    />
  );
};

export default StoragePage;
