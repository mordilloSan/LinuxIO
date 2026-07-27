import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import VMMachinesPage from "./-components/VMMachinesPage";

export const Route = createFileRoute("/_authenticated/vm/machines")({
  validateSearch: (search) => ({
    ...optionalString(search, "vm"),
  }),
  loaderDeps: ({ search }) => ({ vm: search.vm }),
  loader: async ({ context, deps, preload }) => {
    const [vms] = await loadRouteQueries({ context, preload }, [
      linuxio.virt.list.queryOptions(),
    ]);
    const selectedName = vms.some((vm) => vm.name === deps.vm)
      ? deps.vm
      : vms[0]?.name;

    if (!selectedName) return;
    return loadRouteQueries({ context, preload }, [
      linuxio.virt.get.queryOptions(selectedName),
    ]);
  },
  component: VMMachinesPage,
});
