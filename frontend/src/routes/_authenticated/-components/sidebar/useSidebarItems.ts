import { useRouter } from "@tanstack/react-router";

import { hasAccessPolicy, useAccessContext } from "@/hooks/useCapabilities";

import type { SidebarItem } from "./types";

export function useSidebarItems(): SidebarItem[] {
  const access = useAccessContext();
  const router = useRouter();

  return Object.values(router.routesById)
    .filter((route) => {
      const { staticData } = route.options;
      return (
        staticData?.navigation && hasAccessPolicy(staticData.access, access)
      );
    })
    .sort(
      (a, b) =>
        a.options.staticData!.navigation!.position -
        b.options.staticData!.navigation!.position,
    )
    .map((route) => ({
      icon: route.options.staticData!.navigation!.icon,
      params: route.options.staticData!.navigation!.params,
      title: route.options.staticData!.navigation!.title,
      to: route.fullPath,
    }));
}
