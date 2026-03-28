import { Icon } from "@iconify/react";
import React, { useState } from "react";

import DiskOverview from "./DiskOverview";
import LVMManagement from "./LVMManagement";

import { TabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";

const StoragePage: React.FC = () => {
  const [createLVHandler, setCreateLVHandler] = useState<(() => void) | null>(
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
      ]}
      defaultTab="disks"
      urlParam="storageTab"
    />
  );
};

export default StoragePage;
