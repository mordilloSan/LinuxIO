import { Icon } from "@iconify/react";
import React, { useState } from "react";

import { TabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";

import DiskOverview from "./DiskOverview";
import LVMManagement from "./LVMManagement";

const StoragePage: React.FC = () => {
  const [createLVHandler, setCreateLVHandler] = useState<(() => void) | null>(
    null,
  );

  return (
    <TabContainer
      defaultTab="disks"
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
              onClick={createLVHandler}
              size="small"
              startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
              variant="contained"
            >
              Create LV
            </AppButton>
          ) : undefined,
        },
      ]}
      urlParam="storageTab"
    />
  );
};

export default StoragePage;
