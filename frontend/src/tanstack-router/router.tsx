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

type SearchRecord = Record<string, unknown>;

const optionalString = <TKey extends string>(
  search: SearchRecord,
  key: TKey,
): { [P in TKey]?: string } => {
  const value = search[key];
  return typeof value === "string" && value
    ? ({ [key]: value } as { [P in TKey]: string })
    : {};
};

const optionalNumber = <TKey extends string>(
  search: SearchRecord,
  key: TKey,
): { [P in TKey]?: number } => {
  const value = search[key];
  return typeof value === "number"
    ? ({ [key]: value } as { [P in TKey]: number })
    : {};
};

const optionalBoolean = <TKey extends string>(
  search: SearchRecord,
  key: TKey,
): { [P in TKey]?: boolean } => {
  const value = search[key];
  return typeof value === "boolean"
    ? ({ [key]: value } as { [P in TKey]: boolean })
    : {};
};

const validateSearchByRoute = {
  "sign-in": (search: SearchRecord) => ({
    ...optionalString(search, "redirect"),
  }),
  dashboard: (_search: SearchRecord) => ({}),
  network: (search: SearchRecord) => ({
    ...optionalString(search, "iface"),
    ...optionalString(search, "sort"),
    ...optionalString(search, "tab"),
  }),
  updates: (search: SearchRecord) => ({
    ...optionalString(search, "updateTab"),
  }),
  services: (search: SearchRecord) => ({
    ...optionalString(search, "section"),
    ...optionalString(search, "service"),
    ...optionalString(search, "socket"),
    ...optionalString(search, "timer"),
  }),
  logs: (_search: SearchRecord) => ({}),
  storage: (search: SearchRecord) => ({
    ...optionalString(search, "drive"),
    ...optionalString(search, "fs"),
    ...optionalString(search, "storageTab"),
  }),
  docker: (search: SearchRecord) => ({
    ...optionalString(search, "container"),
    ...optionalString(search, "dockerTab"),
  }),
  vm: (search: SearchRecord) => ({ ...optionalString(search, "vmTab") }),
  accounts: (search: SearchRecord) => ({
    ...optionalString(search, "accountsTab"),
    ...optionalBoolean(search, "autoDismissFailedLoginAlert"),
    ...optionalString(search, "failedLoginAlertId"),
    ...optionalString(search, "focusLoginEventId"),
    ...optionalString(search, "user"),
  }),
  shares: (search: SearchRecord) => ({
    ...optionalString(search, "sharesTab"),
  }),
  wireguard: (_search: SearchRecord) => ({}),
  hardware: (_search: SearchRecord) => ({}),
  filebrowser: (search: SearchRecord) => ({
    ...optionalBoolean(search, "enabled"),
    ...optionalString(search, "redirect"),
    ...optionalNumber(search, "tail"),
  }),
  terminal: (_search: SearchRecord) => ({}),
} satisfies Record<
  ProtectedRouteId | "sign-in",
  (search: SearchRecord) => object
>;

/** The router only needs an immutable auth snapshot, not AuthContext methods. */
export type RouterAuthSnapshot = Omit<
  Pick<AuthState, "isAuthenticated" | "isInitialized" | "user">,
  "isInitialized"
> & {
  /** The router mounts only after auth bootstrap. */
  isInitialized: true;
};

/**
 * Dependencies and live state supplied by RouterProvider.
 *
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
export interface ProtectedRouteLoaderOptions {
  context: LinuxIORouterContext;
  preload: boolean;
}

export type ProtectedRouteLoader = (
  options: ProtectedRouteLoaderOptions,
) => Promise<unknown>;

type CatalogEntryFor<TId extends ProtectedRouteId> = Extract<
  (typeof protectedRouteCatalog)[number],
  { id: TId }
>;

/**
 * Components are injected so tests can provide inert components while the app
 * supplies its layouts and pages.
 */
export interface TanStackRouterComponents {
  AuthenticatedLayout?: RouterRouteComponent;
  /**
   * A plain page component because it serves both route-level not-found
   * boundaries and the authenticated catch-all child.
   */
  NotFound?: RouterRouteComponent;
  ProtectedRoutes?: Partial<Record<ProtectedRouteId, RouterRouteComponent>>;
  Root?: RouterRouteComponent;
  SignIn?: RouterRouteComponent;
}

export interface CreateTanStackRouterOptions {
  components?: TanStackRouterComponents;
  context: LinuxIORouterContext;
  history?: RouterHistory;
  /** Test seam confirming child work only runs after access allows it. */
  onAuthorizedProtectedRoute?: (route: ProtectedRouteCatalogEntry) => void;
  /**
   * Explicit per-id loaders. An absent id has no loader at all.
   */
  protectedRouteLoaders?: Partial<
    Record<ProtectedRouteId, ProtectedRouteLoader>
  >;
}

const DefaultNotFound = () => <div>Not found</div>;
const DefaultRoute = () => <div />;

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

/** Pure access guard run by each protected child before route loaders. */
export function requireProtectedRouteAccess(
  route: ProtectedRouteCatalogEntry,
  context: LinuxIORouterContext,
): void {
  if (hasAccessPolicy(route, context.access)) return;
  throw notFound();
}

/** Pure guest guard for signed-in visitors. */
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
 * Builds the code-based router tree from the supplied layouts, pages, guards,
 * and router context. Tests provide the same dependencies with memory history.
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
    validateSearch: validateSearchByRoute["sign-in"],
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
  const protectedRouteOptions = <TId extends ProtectedRouteId>(id: TId) => {
    const catalogRoute = catalogEntry(id);
    return {
      beforeLoad: ({ context }: { context: LinuxIORouterContext }) => {
        requireProtectedRouteAccess(catalogRoute, context);
        options.onAuthorizedProtectedRoute?.(catalogRoute);
      },
      component: components.ProtectedRoutes?.[id] ?? DefaultRoute,
      loader: options.protectedRouteLoaders?.[id],
      notFoundComponent: NotFound,
    };
  };

  const dashboardRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "/",
    validateSearch: validateSearchByRoute.dashboard,
    ...protectedRouteOptions("dashboard"),
  });
  const networkRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "network",
    validateSearch: validateSearchByRoute.network,
    ...protectedRouteOptions("network"),
  });
  const updatesRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "updates",
    validateSearch: validateSearchByRoute.updates,
    ...protectedRouteOptions("updates"),
  });
  const servicesRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "services",
    validateSearch: validateSearchByRoute.services,
    ...protectedRouteOptions("services"),
  });
  const logsRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "logs",
    validateSearch: validateSearchByRoute.logs,
    ...protectedRouteOptions("logs"),
  });
  const storageRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "storage",
    validateSearch: validateSearchByRoute.storage,
    ...protectedRouteOptions("storage"),
  });
  const dockerRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "docker",
    validateSearch: validateSearchByRoute.docker,
    ...protectedRouteOptions("docker"),
  });
  const vmRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "vm",
    validateSearch: validateSearchByRoute.vm,
    ...protectedRouteOptions("vm"),
  });
  const accountsRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "accounts",
    validateSearch: validateSearchByRoute.accounts,
    ...protectedRouteOptions("accounts"),
  });
  const sharesRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "shares",
    validateSearch: validateSearchByRoute.shares,
    ...protectedRouteOptions("shares"),
  });
  const wireguardRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "wireguard",
    validateSearch: validateSearchByRoute.wireguard,
    ...protectedRouteOptions("wireguard"),
  });
  const hardwareRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "hardware",
    validateSearch: validateSearchByRoute.hardware,
    ...protectedRouteOptions("hardware"),
  });
  const filebrowserRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "filebrowser/$",
    validateSearch: validateSearchByRoute.filebrowser,
    ...protectedRouteOptions("filebrowser"),
  });
  const terminalRoute = createRoute({
    getParentRoute: () => authenticatedRoute,
    path: "terminal",
    validateSearch: validateSearchByRoute.terminal,
    ...protectedRouteOptions("terminal"),
  });

  const protectedRoutes = [
    dashboardRoute,
    networkRoute,
    updatesRoute,
    servicesRoute,
    logsRoute,
    storageRoute,
    dockerRoute,
    vmRoute,
    accountsRoute,
    sharesRoute,
    wireguardRoute,
    hardwareRoute,
    filebrowserRoute,
    terminalRoute,
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
    search: { strict: true },
    defaultPreload: "intent",
    defaultPreloadDelay: 150,
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

export type AppRouter = ReturnType<typeof createTanStackRouter>["router"];

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
