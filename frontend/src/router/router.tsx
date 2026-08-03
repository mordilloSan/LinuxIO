import { createRouter } from "@tanstack/react-router";
import type { ElementType } from "react";

import PageLoader from "@/components/loaders/PageLoader";
import type { AccessPolicy } from "@/hooks/useCapabilities";
import type { LinuxIORouterContext } from "@/routes/-auth";
import NotFoundPage from "@/routes/-components/NotFoundPage";
import RouteError from "@/routes/-components/RouteError";
import { routeTree } from "@/routeTree.gen";

interface RouteNavigation {
  icon: ElementType | string;
  params?: { _splat: string };
  position: number;
  title: string;
}

export const router = createRouter({
  context: {
    access: undefined!,
    auth: undefined!,
    isUpdateBlocked: undefined!,
    queryClient: undefined!,
  } satisfies LinuxIORouterContext,
  defaultErrorComponent: RouteError,
  defaultNotFoundComponent: NotFoundPage,
  defaultPendingComponent: PageLoader,
  defaultPreload: "intent",
  defaultPreloadDelay: 50,
  defaultPendingMs: 150,
  defaultPendingMinMs: 0,
  defaultPreloadStaleTime: 0,
  routeTree,
  search: { strict: true },
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }

  interface StaticDataRouteOption {
    access?: AccessPolicy;
    navigation?: RouteNavigation;
  }
}
