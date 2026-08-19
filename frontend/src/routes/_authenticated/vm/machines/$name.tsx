import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";

import VMDetailsPanel from "../-components/VMDetailsPanel";

export const Route = createFileRoute("/_authenticated/vm/machines/$name")({
  context: ({ params }) => ({
    vmQueryOptions: linuxio.virt.get({ name: params.name }),
  }),
  // Path params are automatic loader deps, so no loaderDeps is needed.
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [loaderArgs.context.vmQueryOptions]),
  component: VMDetailRoute,
});

function VMDetailRoute() {
  const vmQueryOptions = Route.useRouteContext({
    select: (context) => context.vmQueryOptions,
  });
  const { data: vm } = useSuspenseQuery({
    ...vmQueryOptions,
    refetchInterval: 5000,
  });

  return <VMDetailsPanel vm={vm} />;
}
