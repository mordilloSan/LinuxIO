import { createFileRoute } from "@tanstack/react-router";

import { linuxio } from "@/api";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import { requireAccess } from "@/routes/-auth";
import { loadRouteQueries } from "@/routes/-loader";
import { optionalString } from "@/routes/-search";

import BtrfsPage from "./-components/btrfs/BtrfsPage";
import {
  DEFAULT_BTRFS_VIEW,
  validateBtrfsView,
} from "./-components/btrfs/btrfsViews";

const access = {
  requiredCapabilities: ["btrfsAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/storage/subvolumes")({
  validateSearch: (search) => ({
    ...optionalString(search, "fs"),
    ...validateBtrfsView(search),
  }),
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.storage.btrfs_list]),
  component: StorageSubvolumesRoute,
  staticData: { access },
});

function StorageSubvolumesRoute() {
  const { fs, view = DEFAULT_BTRFS_VIEW } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <BtrfsPage
      onSelect={(uuid) =>
        void navigate({
          search: (previous) => ({ ...previous, fs: uuid }),
          resetScroll: false,
        })
      }
      onView={(next) =>
        void navigate({
          search: (previous) => ({
            ...previous,
            // Subvolumes is what the tab already means, so it leaves no
            // parameter behind.
            view: next === DEFAULT_BTRFS_VIEW ? undefined : next,
          }),
          resetScroll: false,
        })
      }
      selectedUuid={fs}
      view={view}
    />
  );
}
