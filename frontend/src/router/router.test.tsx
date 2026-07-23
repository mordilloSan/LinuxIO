import { describe, expect, it } from "vitest";

import { router } from "./router";

const leafRoutes = Object.values(router.routesById).filter(
  (route) => route.id !== "__root__" && route.id !== "/_authenticated",
);

describe("generated application router", () => {
  it("uses one global intent-preload policy", () => {
    expect(router.options.defaultPreload).toBe("intent");
    expect(router.options.defaultPreloadDelay).toBe(150);
    expect(router.options.defaultPreloadStaleTime).toBe(0);

    for (const route of Object.values(router.routesById)) {
      expect("preload" in route.options).toBe(false);
      expect("preloadDelay" in route.options).toBe(false);
      expect("preloadStaleTime" in route.options).toBe(false);
    }
  });

  it("contains the complete generated route topology", () => {
    expect(
      leafRoutes
        .map((route) => route.fullPath)
        .sort((a, b) => a.localeCompare(b)),
    ).toEqual(
      [
        "/",
        "/accounts",
        "/docker",
        "/filebrowser/$",
        "/hardware",
        "/logs",
        "/network",
        "/services",
        "/shares",
        "/sign-in",
        "/storage",
        "/terminal",
        "/updates",
        "/vm",
        "/wireguard",
      ].sort((a, b) => a.localeCompare(b)),
    );
  });

  it("gives every protected data page a loader", () => {
    expect(typeof router.routesById["/_authenticated"].options.loader).toBe(
      "function",
    );
    expect(
      leafRoutes
        .filter((route) => route.fullPath !== "/sign-in")
        .filter((route) => typeof route.options.loader !== "function")
        .map((route) => route.id),
    ).toEqual([]);
  });

  it("does not add empty loaders to data-free routes", () => {
    expect(router.routesById["/sign-in"].options.loader).toBeUndefined();
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
