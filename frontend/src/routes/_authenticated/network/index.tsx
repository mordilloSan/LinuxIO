import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

import { linuxio } from "@/api";
import BridgeHandoffDialog from "@/components/network/BridgeHandoffDialog";
import CreateBridgeDialog from "@/components/network/CreateBridgeDialog";
import { RoutedTabActions } from "@/components/tabbar";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import HeaderActions from "@/components/ui/HeaderActions";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalHandoffOperationId, optionalString } from "@/routes/-search";

import NetworkInterfaceList from "./-components/NetworkInterfaceList";

function NetworkInterfacesPage() {
  const [createBridgeOpen, setCreateBridgeOpen] = useState(false);
  const [bridgeHandoffRequested, setBridgeHandoffRequested] = useState(false);
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const bridgeHandoffOpen =
    bridgeHandoffRequested || Boolean(search.handoffOperationId);

  const setHandoffOperationId = (operationId: string | undefined) => {
    return navigate({
      search: (previous) => ({
        ...previous,
        handoffOperationId: operationId,
      }),
      replace: true,
    });
  };

  return (
    <>
      <RoutedTabActions>
        <HeaderActions
          options={
            <AppActionIconButton
              ariaLabel="Move host IP to bridge"
              icon="mdi:lan"
              iconSize={20}
              label="Move host IP to bridge"
              onClick={() => setBridgeHandoffRequested(true)}
            />
          }
          create={
            <AppActionIconButton
              ariaLabel="Create bridge"
              icon="mdi:lan-connect"
              iconSize={20}
              label="Create bridge"
              onClick={() => setCreateBridgeOpen(true)}
            />
          }
        />
      </RoutedTabActions>
      <NetworkInterfaceList />
      <BridgeHandoffDialog
        onClose={() => setBridgeHandoffRequested(false)}
        onOperationIdChange={setHandoffOperationId}
        open={bridgeHandoffOpen}
        operationId={search.handoffOperationId ?? ""}
      />
      <CreateBridgeDialog
        onClose={() => setCreateBridgeOpen(false)}
        onHandoff={() => {
          setCreateBridgeOpen(false);
          setBridgeHandoffRequested(true);
        }}
        open={createBridgeOpen}
      />
    </>
  );
}

export const Route = createFileRoute("/_authenticated/network/")({
  validateSearch: (search) => ({
    ...optionalString(search, "iface"),
    ...optionalHandoffOperationId(search),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.network.get_network_info]),
  component: NetworkInterfacesPage,
});
