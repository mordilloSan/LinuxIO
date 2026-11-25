import { createFileRoute, Outlet, useParams } from "@tanstack/react-router";

import { linuxio } from "@/api";
import { requireAccess } from "@/routes/-auth";
import { loadRouteQueries } from "@/routes/-loader";

import { POOLS_ACCESS } from "../-components/pools/poolsAccess";
import PoolsLayout from "../-components/pools/PoolsLayout";

export const Route = createFileRoute("/_authenticated/storage/pools")({
  beforeLoad: ({ context }) => requireAccess(POOLS_ACCESS, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [linuxio.storage.btrfs_list]),
  component: StoragePoolsRoute,
  staticData: { access: POOLS_ACCESS },
});

function StoragePoolsRoute() {
  const navigate = Route.useNavigate();
  // Undefined while the index child is active, i.e. no pool selected.
  const detail = useParams({
    from: "/_authenticated/storage/pools/$uuid",
    shouldThrow: false,
  });
  return (
    <PoolsLayout
      onSelect={(uuid) =>
        void navigate(
          uuid
            ? { params: { uuid }, to: "/storage/pools/$uuid" }
            : { to: "/storage/pools" },
        )
      }
      selectedUuid={detail?.uuid ?? null}
    >
      <Outlet />
    </PoolsLayout>
  );
}
