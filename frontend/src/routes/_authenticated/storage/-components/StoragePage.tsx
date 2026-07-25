import { Icon } from "@iconify/react";
import { getRouteApi } from "@tanstack/react-router";
import { useState } from "react";

import { TabContainer } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";

import DiskOverview from "./DiskOverview";
import LVMManagement from "./LVMManagement";

const storageRouteApi = getRouteApi("/_authenticated/storage");

const StoragePage = () => {
  const navigate = storageRouteApi.useNavigate();
  const search = storageRouteApi.useSearch();
  const activeTab = search.storageTab === "lvm" ? "lvm" : "disks";
  const [createLVHandler, setCreateLVHandler] = useState<(() => void) | null>(
    null,
  );

  return (
    <TabContainer
      activeTab={activeTab}
      onTabChange={(storageTab) =>
        navigate({
          to: "/storage",
          search: (previous) => ({ ...previous, storageTab }),
        })
      }
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
    />
  );
};

export default StoragePage;
