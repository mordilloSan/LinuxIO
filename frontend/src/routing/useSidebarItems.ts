import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

import { hasAccessPolicy, useAccessContext } from "@/hooks/useCapabilities";
import { coreRoutes } from "@/routes";
import { createRouteIntentPreload } from "@/routing/routeIntentPreload";

export function useSidebarItems() {
  const access = useAccessContext();
  const queryClient = useQueryClient();

  return useMemo(() => {
    const allRoutes = coreRoutes.filter((route) =>
      hasAccessPolicy(route, access),
    );

    return allRoutes
      .filter((route) => route.sidebar)
      .sort(
        (a, b) =>
          (a.sidebar?.position ?? Number.MAX_SAFE_INTEGER) -
          (b.sidebar?.position ?? Number.MAX_SAFE_INTEGER),
      )
      .map((route) => ({
        href: `/${route.path ?? ""}`.replace("/*", ""),
        title: route.sidebar!.title,
        icon: route.sidebar!.icon,
        preload: createRouteIntentPreload(route, queryClient, access),
        preloadDelayMs: route.intentPreload.delayMs,
      }));
  }, [access, queryClient]);
}
