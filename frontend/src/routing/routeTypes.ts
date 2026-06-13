import type { QueryClient } from "@tanstack/react-query";
import type React from "react";

import type { AccessContext, AccessPolicy } from "@/hooks/useCapabilities";

export type RoutePrefetchQuery = (queryClient: QueryClient) => Promise<unknown>;

export interface RouteIntentPreloadPolicy {
  data: boolean;
  delayMs: number;
  route: boolean;
}

export interface RouteWithSidebar extends AccessPolicy {
  children?: RouteWithSidebar[];
  element?: React.ReactNode;
  intentPreload: RouteIntentPreloadPolicy;
  path?: string;
  prefetchDataWhen?: (access: AccessContext) => boolean;
  prefetchQueries?: RoutePrefetchQuery[];
  preload?: () => Promise<unknown>;
  sidebar?: {
    title: string;
    icon: React.ElementType | string;
    position: number;
  };
}
