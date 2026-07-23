import { useMemo } from "react";

import { hasAccessPolicy, useAccessContext } from "@/hooks/useCapabilities";
import {
  protectedRouteCatalog,
  type ProtectedRouteCatalogEntry,
} from "@/routing/protectedRouteCatalog";

export function useSidebarItems() {
  const access = useAccessContext();

  return useMemo(
    () =>
      protectedRouteCatalog
        .filter((route) =>
          hasAccessPolicy(route as ProtectedRouteCatalogEntry, access),
        )
        .sort((a, b) => a.sidebar.position - b.sidebar.position)
        .map((route) => ({
          href:
            ("href" in route.sidebar ? route.sidebar.href : undefined) ??
            `/${route.path}`,
          icon: route.sidebar.icon,
          title: route.sidebar.title,
        })),
    [access],
  );
}
