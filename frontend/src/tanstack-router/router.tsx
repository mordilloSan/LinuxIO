import type { QueryClient } from "@tanstack/react-query";
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  notFound,
  Outlet,
  redirect,
  type RouterHistory,
} from "@tanstack/react-router";
import type { RouteComponent } from "@tanstack/react-router";

import { hasAccessPolicy, type AccessContext } from "@/hooks/useCapabilities";
import {
  protectedRouteCatalog,
  type ProtectedRouteCatalogEntry,
  type ProtectedRouteId,
} from "@/routing/protectedRouteCatalog";
import type { AuthState } from "@/types/auth";

/** The router only needs an immutable auth snapshot, not AuthContext methods. */
export type RouterAuthSnapshot = Omit<
  Pick<AuthState, "isAuthenticated" | "isInitialized" | "user">,
  "isInitialized"
> & {
  /** The future bridge creates/mounts this router only after auth bootstrap. */
  isInitialized: true;
};

/**
 * Dependencies and live state supplied by the eventual RouterProvider bridge.
 *
 * `isUpdateBlocked` deliberately remains a getter: it is consumed by the
 * Phase 2 route-query helper immediately before and after mux readiness.
 */
export interface LinuxIORouterContext {
  queryClient: QueryClient;
  auth: RouterAuthSnapshot;
  access: AccessContext;
  isUpdateBlocked: () => boolean;
}

type LocationWithHref = {
  href: string;
  search: Record<string, unknown>;
};

export type RouterRouteComponent = RouteComponent;
export type ProtectedRouteComponentFactory = (
  route: ProtectedRouteCatalogEntry,
) => RouterRouteComponent;

type TanStackPath<TPath extends ProtectedRouteCatalogEntry["path"]> =
  TPath extends ""
    ? "/"
    : TPath extends `${infer Prefix}/*`
      ? `${Prefix}/$`
      : TPath;

type CatalogEntryFor<TId extends ProtectedRouteId> = Extract<
  (typeof protectedRouteCatalog)[number],
  { id: TId }
>;

/**
 * Components are injected so this foundation can be exercised with inert
 * test components. The live cutover will supply the existing layouts/pages.
 */
export interface TanStackRouterComponents {
  AuthenticatedLayout?: RouterRouteComponent;
  /**
   * A plain page component because it serves both route-level not-found
   * boundaries and the authenticated catch-all child.
   */
  NotFound?: RouterRouteComponent;
  ProtectedRoute?: ProtectedRouteComponentFactory;
  Root?: RouterRouteComponent;
  SignIn?: RouterRouteComponent;
}

export interface CreateTanStackRouterOptions {
  components?: TanStackRouterComponents;
  context: LinuxIORouterContext;
  history?: RouterHistory;
  /** Test seam confirming future child work only runs after access allows it. */
  onAuthorizedProtectedRoute?: (route: ProtectedRouteCatalogEntry) => void;
}

const DefaultNotFound = () => <div>Not found</div>;
const DefaultRoute = () => <div />;

/**
 * The old router's `filebrowser/*` splat has the same semantics as TanStack
 * Router's `filebrowser/$` path syntax.
 */
export function toTanStackRoutePath<
  TPath extends ProtectedRouteCatalogEntry["path"],
>(path: TPath): TanStackPath<TPath> {
  if (path === "") return "/" as TanStackPath<TPath>;
  return (
    path.endsWith("/*") ? `${path.slice(0, -1)}$` : path
  ) as TanStackPath<TPath>;
}

/** Pure parent guard: child beforeLoad/load work never runs after this redirect. */
export function requireAuthenticatedRoute(
  context: LinuxIORouterContext,
  location: Pick<LocationWithHref, "href" | "search">,
): void {
  if (context.auth.isAuthenticated) return;

  const candidate = location.search.redirect;
  const target =
    typeof candidate === "string" && candidate ? candidate : location.href;

  throw redirect({
    replace: true,
    search: { redirect: target },
    to: "/sign-in",
  });
}

/** Pure access guard run by each protected child before future route loaders. */
export function requireProtectedRouteAccess(
  route: ProtectedRouteCatalogEntry,
  context: LinuxIORouterContext,
): void {
  if (hasAccessPolicy(route, context.access)) return;
  throw notFound();
}

/** Pure guest guard matching the current GuestGuard redirect/default behavior. */
export function redirectAuthenticatedGuest(
  context: LinuxIORouterContext,
  location: Pick<LocationWithHref, "search">,
): void {
  if (!context.auth.isAuthenticated) return;

  const candidate = location.search.redirect;
  const target = typeof candidate === "string" && candidate ? candidate : "/";
  throw redirect({ replace: true, to: target });
}

/**
 * Build (but do not mount) the code-based TanStack Router tree.
 *
 * Production continues to render React Router until the live cutover. Keeping
 * construction in a factory lets tests provide memory history and inert
 * components while preserving the exact catalog and guards for that cutover.
 * The eventual AuthContext bridge must wait for `isInitialized === true`
 * before creating this router, matching AuthGuard's current blank bootstrap.
 */
export function createTanStackRouter(options: CreateTanStackRouterOptions) {
  const components = options.components ?? {};
  const NotFound = components.NotFound ?? DefaultNotFound;
  const rootRoute = createRootRouteWithContext<LinuxIORouterContext>()({
    component: components.Root ?? Outlet,
    notFoundComponent: NotFound,
  });

  const signInRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "sign-in",
    beforeLoad: ({ context, location }) =>
      redirectAuthenticatedGuest(context, location),
    component: components.SignIn ?? DefaultRoute,
  });

  const authenticatedRoute = createRoute({
    getParentRoute: () => rootRoute,
    id: "authenticated",
    beforeLoad: ({ context, location }) =>
      requireAuthenticatedRoute(context, location),
    component: components.AuthenticatedLayout ?? Outlet,
  });

  const catalogEntry = <TId extends ProtectedRouteId>(id: TId) => {
    const entry = protectedRouteCatalog.find(
      (candidate) => candidate.id === id,
    );
    if (!entry) throw new Error(`Unknown protected route: ${id}`);
    return entry as CatalogEntryFor<TId>;
  };
  const createProtectedRoute = <
    TCatalogRoute extends ProtectedRouteCatalogEntry,
  >(
    catalogRoute: TCatalogRoute,
  ) =>
    createRoute({
      getParentRoute: () => authenticatedRoute,
      path: toTanStackRoutePath(catalogRoute.path),
      beforeLoad: ({ context }) => {
        requireProtectedRouteAccess(catalogRoute, context);
        options.onAuthorizedProtectedRoute?.(catalogRoute);
      },
      component: components.ProtectedRoute?.(catalogRoute) ?? DefaultRoute,
      notFoundComponent: NotFound,
    });

  const protectedRoutes = [
    createProtectedRoute(catalogEntry("dashboard")),
    createProtectedRoute(catalogEntry("network")),
    createProtectedRoute(catalogEntry("updates")),
    createProtectedRoute(catalogEntry("services")),
    createProtectedRoute(catalogEntry("logs")),
    createProtectedRoute(catalogEntry("storage")),
    createProtectedRoute(catalogEntry("docker")),
    createProtectedRoute(catalogEntry("vm")),
    createProtectedRoute(catalogEntry("accounts")),
    createProtectedRoute(catalogEntry("shares")),
    createProtectedRoute(catalogEntry("wireguard")),
    createProtectedRoute(catalogEntry("hardware")),
    createProtectedRoute(catalogEntry("filebrowser")),
    createProtectedRoute(catalogEntry("terminal")),
  ] as const;

  const protectedNotFoundRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "$",
    component: NotFound,
  });

  const routeTree = rootRoute.addChildren([
    signInRoute,
    authenticatedRoute.addChildren([
      ...protectedRoutes,
      protectedNotFoundRoute,
    ]),
  ]);

  const router = createRouter({
    routeTree,
    context: options.context,
    history: options.history,
    defaultPreload: "intent",
    defaultPreloadStaleTime: 0,
  });

  return {
    authenticatedRoute,
    protectedNotFoundRoute,
    protectedRoutes,
    rootRoute,
    routeTree,
    router,
    signInRoute,
  };
}
