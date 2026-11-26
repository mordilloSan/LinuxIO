import { createFileRoute } from "@tanstack/react-router";

import PoolDetails from "../-components/pools/PoolDetails";
import {
  DEFAULT_POOL_TAB,
  validatePoolTab,
} from "../-components/pools/poolTabs";

export const Route = createFileRoute("/_authenticated/storage/pools/$uuid")({
  validateSearch: validatePoolTab,
  component: StoragePoolRoute,
});

function StoragePoolRoute() {
  const { uuid } = Route.useParams();
  const { tab = DEFAULT_POOL_TAB } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <PoolDetails
      onGone={() => void navigate({ to: "/storage/pools" })}
      onTab={(next) =>
        void navigate({
          search: { tab: next === DEFAULT_POOL_TAB ? undefined : next },
          replace: true,
          resetScroll: false,
        })
      }
      tab={tab}
      uuid={uuid}
    />
  );
}
