import { Icon } from "@iconify/react";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

import { linuxio } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import AppButton from "@/components/ui/AppButton";
import { loadRouteQueries } from "@/routes/-loader";

import LVMManagement from "./-components/LVMManagement";

export const Route = createFileRoute("/_authenticated/storage/lvm")({
  loader: ({ context, preload }) =>
    loadRouteQueries({ context, preload }, [
      linuxio.storage.list_pvs.queryOptions(),
      linuxio.storage.list_vgs.queryOptions(),
      linuxio.storage.list_lvs.queryOptions(),
    ]),
  component: StorageLVMRoute,
});

function StorageLVMRoute() {
  const [createLVHandler, setCreateLVHandler] = useState<(() => void) | null>(
    null,
  );
  const actions = createLVHandler ? (
    <AppButton
      onClick={createLVHandler}
      size="small"
      startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
      variant="contained"
    >
      Create LV
    </AppButton>
  ) : undefined;

  return (
    <>
      <RoutedTabActions>{actions}</RoutedTabActions>
      <LVMManagement
        onMountCreateHandler={(handler) => setCreateLVHandler(() => handler)}
      />
    </>
  );
}
