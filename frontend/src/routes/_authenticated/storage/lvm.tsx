import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

import { linuxio } from "@/api";
import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import { loadRouteQueries } from "@/routes/-loader";

import LVMManagement from "./-components/LVMManagement";

export const Route = createFileRoute("/_authenticated/storage/lvm")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
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
    <AppActionIconButton
      ariaLabel="Create LV"
      icon="mdi:plus"
      iconSize={20}
      label="Create LV"
      onClick={createLVHandler}
    />
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
