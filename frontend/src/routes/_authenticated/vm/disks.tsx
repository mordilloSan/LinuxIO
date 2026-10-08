import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import VMDisksPage from "./-components/VMDisksPage";

export const Route = createFileRoute("/_authenticated/vm/disks")({
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.virt.unused_disks]),
  component: VMDisksPage,
});
