import { Icon } from "@iconify/react";
import React, { useState } from "react";

import DiskOverview from "./DiskOverview";
import LVMManagement from "./LVMManagement";
import NFSMounts from "./NFSMounts";

import { TabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
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
            <AppButton
              variant="contained"
              size="small"
              onClick={createLVHandler}
              startIcon={<Icon icon="mdi:plus" width={20} height={20} />}
            >
              Create LV
            </AppButton>
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
              <AppTooltip
                title={
                  nfsView === "table"
                    ? "Switch to card view"
                    : "Switch to table view"
                }
              >
                <AppIconButton
                  size="small"
                  onClick={() =>
                    setNfsView(nfsView === "table" ? "card" : "table")
                  }
                >
                  {nfsView === "table" ? (
                    <Icon icon="mdi:view-grid" width={20} height={20} />
                  ) : (
                    <Icon icon="mdi:table-row" width={20} height={20} />
                  )}
                </AppIconButton>
              </AppTooltip>
              {mountNFSHandler && (
                <AppButton
                  variant="contained"
                  size="small"
                  onClick={mountNFSHandler}
                  startIcon={<Icon icon="mdi:plus" width={20} height={20} />}
                >
                  Mount NFS
                </AppButton>
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
