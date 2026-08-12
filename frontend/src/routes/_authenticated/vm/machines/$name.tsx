import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import VMDetailsPanel from "../-components/VMDetailsPanel";

export const Route = createFileRoute("/_authenticated/vm/machines/$name")({
  // Path params are automatic loader deps, so no loaderDeps is needed.
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.virt.get({ name: loaderArgs.params.name }),
    ]),
  component: VMDetailRoute,
});

function VMDetailRoute() {
  const { name } = Route.useParams();
  const { data: vm } = useSuspenseQuery({
    ...linuxio.virt.get({ name }),
    refetchInterval: 5000,
  });

  return <VMDetailsPanel vm={vm} />;
}
