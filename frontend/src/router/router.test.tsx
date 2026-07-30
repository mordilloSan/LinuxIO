import type { AnyRoute } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import PageLoader from "@/components/loaders/PageLoader";
import NotFoundPage from "@/routes/-components/NotFoundPage";
import RouteError from "@/routes/-components/RouteError";

import { router } from "./router";

const applicationRoutes = Object.values(router.routesById).filter(
  (route) => route.id !== "__root__" && route.id !== "/_authenticated",
);

function routeOrAncestorHasLoader(route: AnyRoute): boolean {
  let current: AnyRoute | undefined = route;
  while (current) {
    if (typeof current.options.loader === "function") return true;
    current = current.parentRoute;
  }
  return false;
}

describe("generated application router", () => {
  it("uses one global intent-preload policy", () => {
    expect(router.options.defaultPreload).toBe("intent");
    expect(router.options.defaultPreloadDelay).toBeTypeOf("number");
    expect(router.options.defaultPendingMs).toBeTypeOf("number");
    expect(router.options.defaultPendingMinMs).toBeTypeOf("number");
    expect(router.options.defaultPreloadStaleTime).toBeTypeOf("number");
    expect(router.options.defaultErrorComponent).toBe(RouteError);
    expect(router.options.defaultNotFoundComponent).toBe(NotFoundPage);
    expect(router.options.defaultPendingComponent).toBe(PageLoader);

    for (const route of Object.values(router.routesById)) {
      expect("preload" in route.options).toBe(false);
      expect("preloadDelay" in route.options).toBe(false);
      expect("preloadStaleTime" in route.options).toBe(false);
    }
  });

  it("contains the complete generated route topology", () => {
    expect(
      applicationRoutes
        .map((route) => route.fullPath)
        .sort((a, b) => a.localeCompare(b)),
    ).toEqual(
      [
        "/",
        "/accounts",
        "/accounts/",
        "/accounts/groups",
        "/docker",
        "/docker/",
        "/docker/compose",
        "/docker/containers",
        "/docker/images",
        "/docker/networks",
        "/docker/volumes",
        "/filebrowser/$",
        "/hardware",
        "/logs",
        "/network",
        "/services",
        "/services/",
        "/services/sockets",
        "/services/timers",
        "/shares",
        "/shares/",
        "/shares/mounts",
        "/sign-in",
        "/storage",
        "/storage/",
        "/storage/lvm",
        "/terminal",
        "/updates",
        "/updates/",
        "/updates/history",
        "/vm",
        "/vm/",
        "/vm/images",
        "/vm/machines",
        "/vm/machines/",
        "/vm/machines/$name",
        "/vm/networks",
        "/wireguard",
      ].sort((a, b) => a.localeCompare(b)),
    );
  });

  it("gives every protected leaf page a loader in its route branch", () => {
    const protectedLeafRoutes = Object.values(router.routesById)
      .filter((route) => route.id.startsWith("/_authenticated"))
      .filter((route) => !route.children);

    expect(
      protectedLeafRoutes
        .filter((route) => !routeOrAncestorHasLoader(route))
        .map((route) => route.id),
    ).toEqual([]);
  });

  it("does not add empty loaders to data-free layout routes", () => {
    expect(router.routesById["/sign-in"].options.loader).toBeUndefined();
    expect(router.routesById["/_authenticated"].options.loader).toBeUndefined();
    for (const routeId of [
      "/_authenticated/accounts",
      "/_authenticated/docker",
      "/_authenticated/services",
      "/_authenticated/shares",
      "/_authenticated/storage",
      "/_authenticated/updates",
      "/_authenticated/vm/machines",
    ] as const) {
      expect(router.routesById[routeId].options.loader).toBeUndefined();
    }
    expect(
      typeof router.routesById["/_authenticated"].options.notFoundComponent,
    ).toBe("function");
  });

  it("keeps navigation metadata on the routes in sidebar order", () => {
    const navigation = Object.values(router.routesById)
      .flatMap((route) => {
        const item = route.options.staticData?.navigation;
        return item ? [item] : [];
      })
      .sort((a, b) => a.position - b.position);

    expect(navigation.map((item) => item.title)).toEqual([
      "Dashboard",
      "Network",
      "Updates",
      "Services",
      "Logs",
      "Storage",
      "Docker",
      "VMs",
      "Accounts",
      "Shares",
      "Wireguard",
      "Hardware",
      "Navigator",
      "Terminal",
    ]);
  });

  it("co-locates protected access policy with its route", () => {
    expect(
      router.routesById["/_authenticated/docker"].options.staticData,
    ).toMatchObject({
      access: { requiredCapabilities: ["dockerAvailable"] },
    });
    expect(
      router.routesById["/_authenticated/vm"].options.staticData,
    ).toMatchObject({
      access: {
        requiredCapabilities: ["libvirtAvailable"],
        requiresPrivileged: true,
      },
    });
    expect(
      router.routesById["/_authenticated/wireguard"].options.staticData,
    ).toMatchObject({
      access: {
        requiredCapabilities: ["wireguardAvailable"],
        requiresPrivileged: true,
      },
    });
  });
});
