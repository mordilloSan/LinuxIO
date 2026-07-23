import { QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  Outlet,
  RouterProvider,
  type NavigateOptions,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { emptyCapabilityState } from "@/api/capabilities";
import { coreRoutes } from "@/routes";
import { protectedRouteCatalog } from "@/routing/protectedRouteCatalog";
import {
  createTanStackRouter,
  toTanStackRoutePath,
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
    path: "filebrowser/*",
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
      ProtectedRoute: (route) => () => (
        <div data-testid={`route-${route.id}`}>{route.id}</div>
      ),
      Root: Outlet,
      SignIn: () => <div data-testid="sign-in">sign-in</div>,
    },
    onAuthorizedProtectedRoute: onAuthorizedProtectedRoute
      ? (route) => onAuthorizedProtectedRoute(route.id)
      : undefined,
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

describe("TanStack Router foundation", () => {
  it("keeps the complete protected topology and policy in parity with coreRoutes", () => {
    const {
      router,
      protectedNotFoundRoute,
      protectedRoutes,
      rootRoute,
      signInRoute,
    } = makeRouter("/");

    expect(coreRoutes.map(routeTopology)).toEqual(expectedProtectedTopology);
    expect(protectedRouteCatalog.map(routeTopology)).toEqual(
      expectedProtectedTopology,
    );
    expect(protectedRoutes.map((route) => route.fullPath)).toEqual(
      protectedRouteCatalog.map((route) => {
        const path = toTanStackRoutePath(route.path);
        return path === "/" ? path : `/${path}`;
      }),
    );
    expect(rootRoute.id).toBe("__root__");
    expect(signInRoute.fullPath).toBe("/sign-in");
    expect(protectedNotFoundRoute.fullPath).toBe("/$");
    expect(router.options.defaultPreload).toBe("intent");
    expect(router.options.defaultPreloadStaleTime).toBe(0);
  });

  it("redirects an unauthenticated protected deep link with its full target", async () => {
    const routerContext = context({
      auth: { isAuthenticated: false, isInitialized: true, user: null },
    });
    const { router } = makeRouter(
      "/network?tab=interfaces&sort=name#routes",
      routerContext,
    );

    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByTestId("sign-in")).toBeVisible());
    expect(router.state.location.pathname).toBe("/sign-in");
    expect(
      (router.state.location.search as { redirect?: string }).redirect,
    ).toBe("/network?tab=interfaces&sort=name#routes");
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

  it("allows authorized routes before their future loader boundary", async () => {
    const marker = vi.fn();
    const { router } = makeRouter("/network", context(), marker);
    render(<RouterProvider router={router} />);

    await waitFor(() =>
      expect(screen.getByTestId("route-network")).toBeVisible(),
    );
    expect(screen.getByTestId("authenticated-layout")).toBeVisible();
    expect(marker).toHaveBeenCalledWith("network");
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
    expect(match?.params._splat).toBe("home/miguel/Project Files/readme.md");
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
