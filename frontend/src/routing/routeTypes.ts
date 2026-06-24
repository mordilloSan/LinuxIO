import type { QueryClient } from "@tanstack/react-query";
import type React from "react";

import type { AccessContext, AccessPolicy } from "@/hooks/useCapabilities";

/**
 * A single data-prefetch task bound to a `QueryClient`. Produced by `routeQuery`
 * (`@/routing/routeIntentPreload`) and collected on a route's
 * {@link RouteWithSidebar.prefetchQueries}. Resolves when the prefetch settles;
 * callers treat rejection as best-effort (intent prefetch is speculative).
 */
export type RoutePrefetchQuery = (queryClient: QueryClient) => Promise<unknown>;

/**
 * Describes *what* a route preloads on navigation intent and *how eagerly*.
 * Routes usually reference a ready-made preset from `ROUTE_INTENT_PRELOAD`
 * (`@/routing/routeIntentPreload`) rather than constructing this by hand.
 */
export interface RouteIntentPreloadPolicy {
  /** Also prefetch the route's React Query data (its `prefetchQueries`). */
  data: boolean;
  /**
   * Debounce, in milliseconds, between the intent signal (hover/focus) and the
   * preload firing. Immediate triggers (mousedown/touch) bypass this delay.
   */
  delayMs: number;
  /** Preload the route's lazy code chunk (its `preload()` loader). */
  route: boolean;
}

/**
 * A route node in the sidebar-aware route tree. Beyond standard routing
 * (`path`, `element`, `children`) and the inherited {@link AccessPolicy} gate,
 * it carries the metadata that drives intent preloading and sidebar rendering.
 */
export interface RouteWithSidebar extends AccessPolicy {
  /** Nested child routes, each itself a {@link RouteWithSidebar}. */
  children?: RouteWithSidebar[];
  /** The element rendered when this route is active. */
  element?: React.ReactNode;
  /** Intent-preload policy — see {@link RouteIntentPreloadPolicy}. */
  intentPreload: RouteIntentPreloadPolicy;
  /** Route path segment, as understood by the router. */
  path?: string;
  /**
   * Optional access gate for data prefetch: when present, `prefetchQueries` only
   * run if this returns `true` for the current capability context (e.g. only
   * prefetch updates when PackageKit is available). Absent means "always".
   */
  prefetchDataWhen?: (access: AccessContext) => boolean;
  /** Data prefetch tasks run when the policy enables `data`. */
  prefetchQueries?: RoutePrefetchQuery[];
  /** Loads the route's lazy code chunk; run when the policy enables `route`. */
  preload?: () => Promise<unknown>;
  /** Sidebar presentation; absent for routes that don't appear in the nav. */
  sidebar?: {
    title: string;
    icon: React.ElementType | string;
    position: number;
  };
}
