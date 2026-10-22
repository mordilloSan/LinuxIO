import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { requireAccess } from "@/routes/-auth";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import BtrfsPage from "./-components/btrfs/BtrfsPage";

const access = {
  requiredCapabilities: ["btrfsAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/storage/snapshots")({
  validateSearch: (search) => optionalString(search, "fs"),
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.storage.btrfs_list]),
  component: StorageSnapshotsRoute,
  staticData: { access },
});

function StorageSnapshotsRoute() {
  const { fs } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <BtrfsPage
      selectedUuid={fs}
      onSelect={(uuid) =>
        void navigate({ search: { fs: uuid }, resetScroll: false })
      }
    />
  );
}
