import { createFileRoute, Outlet } from "@tanstack/react-router";

import { RoutedTabLayout, type RoutedTab } from "@/components/tabbar";
import { hasAccessPolicy, useAccessContext } from "@/hooks/useCapabilities";
import { NetworkIcon } from "@/icons/svg";

import { NETWORK_TABS } from "./-components/networkTabs";
import { Route as WireguardRoute } from "./wireguard";

function NetworkLayout() {
  const access = useAccessContext();
  const tabs: readonly RoutedTab[] = hasAccessPolicy(
    WireguardRoute.options.staticData?.access,
    access,
  )
    ? NETWORK_TABS
    : NETWORK_TABS.filter((tab) => tab.to !== "/network/wireguard");
  return (
    <RoutedTabLayout tabs={tabs}>
      <Outlet />
    </RoutedTabLayout>
  );
}

export const Route = createFileRoute("/_authenticated/network")({
  component: NetworkLayout,
  staticData: {
    navigation: {
      icon: NetworkIcon,
      position: 10,
      title: "Network",
    },
  },
});
