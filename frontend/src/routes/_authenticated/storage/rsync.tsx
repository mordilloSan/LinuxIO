import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { requireAccess } from "@/routes/-auth";
import { loadRouteQueries } from "@/routes/-loader";

import RsyncPage from "./-components/RsyncPage";

const access = { requiresPrivileged: true } satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/shares/rsync")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.shares.get_rsync,
      linuxio.shares.get_rsync_ssh({ port: 22 }),
    ]),
  component: RsyncPage,
  staticData: { access },
});
