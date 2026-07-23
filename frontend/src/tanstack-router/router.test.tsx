import { QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  lazyRouteComponent,
  type RouteComponent,
  Outlet,
  RouterProvider,
  type NavigateOptions,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import { protectedRouteComponents } from "@/routes";
import {
  protectedRouteCatalog,
  type ProtectedRouteId,
} from "@/routing/protectedRouteCatalog";
import { protectedRouteLoaders } from "@/routing/protectedRouteLoaders";
import {
  createTanStackRouter,
  type CreateTanStackRouterOptions,
  type LinuxIORouterContext,
} from "@/tanstack-router/router";
const expectedProtectedTopology = [
  { path: "", sidebar: { position: 0, title: "Dashboard" } },
  { path: "network", sidebar: { position: 10, title: "Network" } },
  { path: "updates", sidebar: { position: 20, title: "Updates" } },
  { path: "services", sidebar: { position: 30, title: "Services" } },
  { path: "logs", sidebar: { position: 35, title: "Logs" } },
  { path: "storage", sidebar: { position: 40, title: "Storage" } },
  {
    path: "docker",
    requiredCapabilities: ["dockerAvailable"],
    sidebar: { position: 50, title: "Docker" },
  },
  {
    path: "vm",
    requiredCapabilities: ["libvirtAvailable"],
    requiresPrivileged: true,
    sidebar: { position: 55, title: "VMs" },
  },
  { path: "accounts", sidebar: { position: 60, title: "Accounts" } },
  { path: "shares", sidebar: { position: 70, title: "Shares" } },
  {
    path: "wireguard",
    requiredCapabilities: ["wireguardAvailable"],
    requiresPrivileged: true,
    sidebar: { position: 80, title: "Wireguard" },
  },
  {
    path: "hardware",
    requiredCapabilities: ["lmSensorsAvailable"],
    sidebar: { position: 90, title: "Hardware" },
  },
  {
    path: "filebrowser/$",
    sidebar: { position: 100, title: "Navigator" },
  },
  { path: "terminal", sidebar: { position: 110, title: "Terminal" } },
];

function routeTopology(route: {
  path?: string;
  requiredCapabilities?: readonly string[];
  requiresPrivileged?: boolean;
  sidebar?: { position: number; title: string };
}) {
  return {
    path: route.path,
    ...(route.requiredCapabilities
      ? { requiredCapabilities: route.requiredCapabilities }
      : {}),
    ...(route.requiresPrivileged ? { requiresPrivileged: true } : {}),
    ...(route.sidebar
      ? {
          sidebar: {
            position: route.sidebar.position,
            title: route.sidebar.title,
          },
        }
      : {}),
  };
}
const protectedRouteTestComponents = Object.fromEntries(
  protectedRouteCatalog.map((route) => [
    route.id,
    () => <div data-testid={`route-${route.id}`}>{route.id}</div>,
  ]),
) as unknown as Record<ProtectedRouteId, RouteComponent>;

const routers: Array<ReturnType<typeof createTanStackRouter>["router"]> = [];

afterEach(() => {
  for (const router of routers.splice(0)) {
    router.history.destroy();
    router.options.context.queryClient.clear();
  }
});

function context(
  overrides: Partial<LinuxIORouterContext> = {},
): LinuxIORouterContext {
  return {
    queryClient: new QueryClient(),
    auth: {
      isAuthenticated: true,
      isInitialized: true,
      user: { id: "root", name: "root" },
    },
    access: { ...emptyCapabilityState, privileged: true },
    isUpdateBlocked: () => false,
    ...overrides,
  };
}

function makeRouter(
  initialEntry: string,
  routerContext = context(),
  onAuthorizedProtectedRoute?: (id: string) => void,
  protectedRouteLoaders?: CreateTanStackRouterOptions["protectedRouteLoaders"],
) {
  const result = createTanStackRouter({
    context: routerContext,
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
    components: {
      AuthenticatedLayout: () => (
        <div data-testid="authenticated-layout">
          <Outlet />
        </div>
      ),
      NotFound: () => <div>router-not-found</div>,
      ProtectedRoutes: protectedRouteTestComponents,
      Root: Outlet,
      SignIn: () => <div data-testid="sign-in">sign-in</div>,
    },
    onAuthorizedProtectedRoute: onAuthorizedProtectedRoute
      ? (route) => onAuthorizedProtectedRoute(route.id)
      : undefined,
    protectedRouteLoaders,
  });
  routers.push(result.router);
  return result;
}

type FoundationRouter = ReturnType<typeof createTanStackRouter>["router"];

const knownRoute: NavigateOptions<FoundationRouter, string, "/network"> = {
  to: "/network",
};
const impossibleRoute: NavigateOptions<
  FoundationRouter,
  string,
  "/not-a-linuxio-route"
> = {
  // @ts-expect-error The concrete route tree must reject unknown paths.
  to: "/not-a-linuxio-route",
};

void knownRoute;
void impossibleRoute;

const validNetworkSearch: NavigateOptions<
  FoundationRouter,
  string,
  "/network"
> = {
  to: "/network",
  search: { iface: "eth0" },
};
const invalidNetworkSearchValue: NavigateOptions<
  FoundationRouter,
  string,
  "/network"
> = {
  to: "/network",
  search: {
    // @ts-expect-error Network iface accepts strings only.
    iface: true,
  },
};
const invalidNetworkSearchKey: NavigateOptions<
  FoundationRouter,
  string,
  "/network"
> = {
  to: "/network",
  search: {
    // @ts-expect-error Network does not accept unrelated search keys.
    accountsTab: "users",
  },
};

void validNetworkSearch;
void invalidNetworkSearchValue;
void invalidNetworkSearchKey;

describe("TanStack Router foundation", () => {
  it("keeps the complete protected topology and policy", () => {
    const {
      router,
      protectedNotFoundRoute,
      protectedRoutes,
      rootRoute,
      signInRoute,
    } = makeRouter("/");

    expect(protectedRouteCatalog.map(routeTopology)).toEqual(
      expectedProtectedTopology,
    );
    expect(protectedRoutes.map((route) => route.fullPath)).toEqual(
      protectedRouteCatalog.map((route) =>
        route.path ? `/${route.path}` : "/",
      ),
    );
    expect(rootRoute.id).toBe("__root__");
    expect(signInRoute.fullPath).toBe("/sign-in");
    expect(protectedNotFoundRoute.fullPath).toBe("/$");
    expect(router.options.defaultPreload).toBe("intent");
    expect(router.options.defaultPreloadDelay).toBe(150);
    expect(router.options.defaultPreloadStaleTime).toBe(0);
  });

  it("registers exactly four data loaders and native lazy pages", () => {
    const { protectedRoutes } = makeRouter(
      "/",
      context(),
      undefined,
      protectedRouteLoaders,
    );

    expect(
      protectedRoutes
        .filter((route) => route.options.loader)
        .map((route) => route.fullPath),
    ).toEqual(["/", "/network", "/updates", "/services"]);
    expect(Object.keys(protectedRouteLoaders)).toEqual([
      "dashboard",
      "network",
      "services",
      "updates",
    ]);
    expect(
      protectedRoutes
        .filter((route) => !route.options.loader)
        .map((route) => route.fullPath),
    ).toEqual([
      "/logs",
      "/storage",
      "/docker",
      "/vm",
      "/accounts",
      "/shares",
      "/wireguard",
      "/hardware",
      "/filebrowser/$",
      "/terminal",
    ]);

    for (const component of Object.values(protectedRouteComponents)) {
      expect((component as { preload?: unknown }).preload).toEqual(
        expect.any(Function),
      );
    }
  });

  it("natively preloads Logs, Storage, and Docker chunks without loaders or queries", async () => {
    const queryClient = new QueryClient();
    const logsImporter = vi.fn(async () => ({
      default: () => <div>logs</div>,
    }));
    const storageImporter = vi.fn(async () => ({
      default: () => <div>storage</div>,
    }));
    const dockerImporter = vi.fn(async () => ({
      default: () => <div>docker</div>,
    }));
    const result = createTanStackRouter({
      components: {
        AuthenticatedLayout: Outlet,
        ProtectedRoutes: {
          ...protectedRouteTestComponents,
          docker: lazyRouteComponent(dockerImporter),
          logs: lazyRouteComponent(logsImporter),
          storage: lazyRouteComponent(storageImporter),
        },
        Root: Outlet,
      },
      context: context({
        access: {
          ...emptyCapabilityState,
          dockerAvailable: true,
          privileged: true,
        },
        queryClient,
      }),
      history: createMemoryHistory({ initialEntries: ["/"] }),
    });
    routers.push(result.router);
    const ensureQueryData = vi.spyOn(queryClient, "ensureQueryData");

    await Promise.all([
      result.router.preloadRoute({ to: "/docker" }),
      result.router.preloadRoute({ to: "/logs" }),
      result.router.preloadRoute({ to: "/storage" }),
    ]);

    expect(dockerImporter).toHaveBeenCalledTimes(1);
    expect(logsImporter).toHaveBeenCalledTimes(1);
    expect(storageImporter).toHaveBeenCalledTimes(1);
    expect(ensureQueryData).not.toHaveBeenCalled();
    expect(
      result.protectedRoutes
        .filter((route) =>
          ["/docker", "/logs", "/storage"].includes(route.fullPath),
        )
        .every((route) => route.options.loader === undefined),
    ).toBe(true);
  });

  it("denies protected preload before its lazy importer and loader execute", async () => {
    const dockerImporter = vi.fn(async () => ({
      default: () => <div>docker</div>,
    }));
    const dockerLoader = vi.fn(async () => undefined);
    const result = createTanStackRouter({
      components: {
        AuthenticatedLayout: Outlet,
        ProtectedRoutes: {
          ...protectedRouteTestComponents,
          docker: lazyRouteComponent(dockerImporter),
        },
        Root: Outlet,
      },
      context: context({
        access: { ...emptyCapabilityState, privileged: true },
      }),
      history: createMemoryHistory({ initialEntries: ["/"] }),
      protectedRouteLoaders: { docker: dockerLoader },
    });
    routers.push(result.router);

    await expect(
      result.router.preloadRoute({ to: "/docker" }),
    ).resolves.toBeDefined();

    expect(dockerImporter).not.toHaveBeenCalled();
    expect(dockerLoader).not.toHaveBeenCalled();
  });

  it("redirects an unauthenticated protected deep link with its full target", async () => {
    const loader = vi.fn(async () => undefined);
    const routerContext = context({
      auth: { isAuthenticated: false, isInitialized: true, user: null },
    });
    const { router } = makeRouter(
      "/network?tab=interfaces&sort=name#routes",
      routerContext,
      undefined,
      { network: loader },
    );

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByTestId("sign-in")).toBeVisible());
    expect(router.state.location.pathname).toBe("/sign-in");
    expect(
      (router.state.location.search as { redirect?: string }).redirect,
    ).toBe("/network?tab=interfaces&sort=name#routes");
    expect(loader).not.toHaveBeenCalled();
  });

  it("validates JSON-first values into the strict Accounts schema", async () => {
    const { router } = makeRouter(
      "/accounts?accountsTab=users&user=alice&autoDismissFailedLoginAlert=true&unexpected=ignored",
    );

    render(<RouterProvider router={router} />);

    await waitFor(() =>
      expect(screen.getByTestId("route-accounts")).toBeVisible(),
    );
    const accountsMatch = router.state.matches.at(-1);
    expect(accountsMatch?._strictSearch).toEqual({
      accountsTab: "users",
      autoDismissFailedLoginAlert: true,
      user: "alice",
    });

    await router.navigate({ to: "/accounts", search: true });

    expect(router.state.location.search).toEqual({
      accountsTab: "users",
      autoDismissFailedLoginAlert: true,
      user: "alice",
    });
  });

  it("re-evaluates a bootstrapped protected location when auth becomes available", async () => {
    const { router } = makeRouter(
      "/network?tab=interfaces#routes",
      context({
        auth: { isAuthenticated: false, isInitialized: true, user: null },
      }),
    );
    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByTestId("sign-in")).toBeVisible());

    router.update({ context: context() });
    await router.invalidate();

    await waitFor(() =>
      expect(screen.getByTestId("route-network")).toBeVisible(),
    );
    expect(router.state.location.href).toBe("/network?tab=interfaces#routes");
  });

  it("re-evaluates access gates when the live router context changes", async () => {
    const { router } = makeRouter(
      "/docker",
      context({ access: { ...emptyCapabilityState, privileged: true } }),
    );
    render(<RouterProvider router={router} />);

    await waitFor(() =>
      expect(screen.getByText("router-not-found")).toBeVisible(),
    );

    router.update({
      context: context({
        access: {
          ...emptyCapabilityState,
          dockerAvailable: true,
          privileged: true,
        },
      }),
    });
    await router.invalidate();

    await waitFor(() =>
      expect(screen.getByTestId("route-docker")).toBeVisible(),
    );
  });

  it("preserves an existing encoded file-browser redirect target", async () => {
    const routerContext = context({
      auth: { isAuthenticated: false, isInitialized: true, user: null },
    });
    const { router } = makeRouter(
      "/filebrowser/tmp?redirect=%2Ffilebrowser%2Fsrv%2Fmy%2520files%3Fview%3Ddetails%23preview",
      routerContext,
    );

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByTestId("sign-in")).toBeVisible());
    expect(
      (router.state.location.search as { redirect?: string }).redirect,
    ).toBe("/filebrowser/srv/my%20files?view=details#preview");
  });

  it("returns authenticated visitors from sign-in to the redirect target or dashboard", async () => {
    const redirected = makeRouter(
      "/sign-in?redirect=%2Fnetwork%3Ftab%3Dinterfaces%23routes",
    );
    render(<RouterProvider router={redirected.router} />);

    await waitFor(() =>
      expect(screen.getByTestId("route-network")).toBeVisible(),
    );
    expect(redirected.router.state.location.href).toBe(
      "/network?tab=interfaces#routes",
    );

    const defaultTarget = makeRouter("/sign-in");
    render(<RouterProvider router={defaultTarget.router} />);

    await waitFor(() =>
      expect(screen.getByTestId("route-dashboard")).toBeVisible(),
    );
    expect(defaultTarget.router.state.location.pathname).toBe("/");
  });

  it("denies capability and privilege routes before their child marker runs", async () => {
    const marker = vi.fn();
    const capabilityDenied = makeRouter(
      "/docker",
      context({
        access: { ...emptyCapabilityState, privileged: true },
      }),
      marker,
    );
    render(<RouterProvider router={capabilityDenied.router} />);

    await waitFor(() =>
      expect(screen.getByText("router-not-found")).toBeVisible(),
    );
    expect(screen.getByTestId("authenticated-layout")).toBeVisible();
    expect(marker).not.toHaveBeenCalled();

    const privilegeDenied = makeRouter(
      "/wireguard",
      context({
        access: {
          ...emptyCapabilityState,
          wireguardAvailable: true,
          privileged: false,
        },
      }),
      marker,
    );
    render(<RouterProvider router={privilegeDenied.router} />);

    await waitFor(() =>
      expect(screen.getAllByText("router-not-found")).toHaveLength(2),
    );
    expect(screen.getAllByTestId("authenticated-layout")).toHaveLength(2);
    expect(marker).not.toHaveBeenCalled();
  });

  it("runs protected access before the Dashboard/Network loader boundary", async () => {
    const marker = vi.fn();
    const loader = vi.fn(async () => undefined);
    const { router } = makeRouter("/network", context(), marker, {
      network: loader,
    });
    render(<RouterProvider router={router} />);

    await waitFor(() =>
      expect(screen.getByTestId("route-network")).toBeVisible(),
    );
    expect(screen.getByTestId("authenticated-layout")).toBeVisible();
    expect(marker).toHaveBeenCalledWith("network");
    expect(loader).toHaveBeenCalledTimes(1);
    expect(marker.mock.invocationCallOrder[0]).toBeLessThan(
      loader.mock.invocationCallOrder[0],
    );
  });

  it("matches encoded file-browser paths with the protected splat route", async () => {
    const { protectedRoutes, router } = makeRouter(
      "/filebrowser/home/miguel/Project%20Files/readme.md",
    );
    const fileBrowserRoute = protectedRoutes.find(
      (route) => route.fullPath === "/filebrowser/$",
    );
    expect(fileBrowserRoute).toBeDefined();

    render(<RouterProvider router={router} />);

    await waitFor(() =>
      expect(screen.getByTestId("route-filebrowser")).toBeVisible(),
    );
    expect(screen.queryByText("router-not-found")).not.toBeInTheDocument();
    const match = router.state.matches.find(
      (candidate) => candidate.routeId === fileBrowserRoute?.id,
    );
    expect(match?.params).toMatchObject({
      _splat: "home/miguel/Project Files/readme.md",
    });
  });

  it("passes the exact QueryClient and context into the router", () => {
    const queryClient = new QueryClient();
    const isUpdateBlocked = () => true;
    const routerContext = context({ queryClient, isUpdateBlocked });
    const { router } = makeRouter("/network", routerContext);

    expect(router.options.context).toBe(routerContext);
    expect(router.options.context.queryClient).toBe(queryClient);
    expect(router.options.context.isUpdateBlocked()).toBe(true);
  });

  it("renders the authenticated not-found topology for unmatched paths", async () => {
    const { router } = makeRouter("/not-a-route");
    render(<RouterProvider router={router} />);

    await waitFor(() =>
      expect(screen.getByText("router-not-found")).toBeVisible(),
    );
    expect(screen.getByTestId("authenticated-layout")).toBeVisible();
  });
});
