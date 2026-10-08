import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import VMNetworksPage from "./-components/VMNetworksPage";

export const Route = createFileRoute("/_authenticated/vm/networks")({
  loader: (loaderArgs) => loadRouteQueries(loaderArgs, [linuxio.virt.networks]),
  component: VMNetworksPage,
});
