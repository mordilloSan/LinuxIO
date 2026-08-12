import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { NetworkIcon } from "@/icons/svg";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import NetworkInterfaceList from "./-components/NetworkInterfaceList";

export const Route = createFileRoute("/_authenticated/network")({
  validateSearch: (search) => ({
    ...optionalString(search, "iface"),
    ...optionalString(search, "sort"),
    ...optionalString(search, "tab"),
  }),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.network.get_network_info]),
  component: NetworkInterfaceList,
  staticData: {
    navigation: {
      icon: NetworkIcon,
      position: 10,
      title: "Network",
    },
  },
});
