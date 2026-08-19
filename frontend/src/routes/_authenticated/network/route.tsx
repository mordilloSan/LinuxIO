import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { RoutedTabLayout } from "@/components/tabbar";
import { NetworkIcon } from "@/icons/svg";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import NetworkInterfaceList from "./-components/NetworkInterfaceList";
import { NETWORK_TABS } from "./-components/networkTabs";

// Network has no child routes, so it renders the interface list as tab
// content directly rather than routing to it through an Outlet the way
// docker and updates route between siblings.
function NetworkLayout() {
  return (
    <RoutedTabLayout tabs={NETWORK_TABS}>
      <NetworkInterfaceList />
    </RoutedTabLayout>
  );
}

export const Route = createFileRoute("/_authenticated/network")({
  validateSearch: (search) => ({
    ...optionalString(search, "iface"),
    ...optionalString(search, "tab"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.network.get_network_info]),
  component: NetworkLayout,
  staticData: {
    navigation: {
      icon: NetworkIcon,
      position: 10,
      title: "Network",
    },
  },
});
