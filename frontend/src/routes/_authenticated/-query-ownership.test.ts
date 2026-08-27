import { readFileSync } from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

import { sourceFiles } from "@/test/sourceFiles";

const authenticatedRoutes = path.resolve(
  process.cwd(),
  "src/routes/_authenticated",
);
const readRouteSource = (relativePath: string) =>
  readFileSync(path.join(authenticatedRoutes, relativePath), "utf8");

describe("targeted route query ownership", () => {
  it("single-sources route-derived query options through route context", () => {
    const cases = [
      {
        consumers: ["vm/machines/$name.tsx"],
        fields: ["vmQueryOptions"],
        route: "vm/machines/$name.tsx",
      },
      {
        consumers: ["filebrowser/-components/FileBrowserPage.tsx"],
        fields: ["fileBrowserListingQueryOptions"],
        route: "filebrowser/$.tsx",
      },
      {
        consumers: [
          "accounts/-components/UsersTab.tsx",
          "accounts/-components/components/UserAccountDetails.tsx",
        ],
        fields: [
          "listUsersQueryOptions",
          "selectedUserDetailsQueryOptions",
          "selectedUserLoginsQueryOptions",
        ],
        route: "accounts/index.tsx",
      },
      {
        consumers: ["services/index.tsx"],
        fields: ["listQueryOptions", "selectedQueryOptions"],
        route: "services/index.tsx",
      },
      {
        consumers: ["services/timers.tsx"],
        fields: ["listQueryOptions", "selectedQueryOptions"],
        route: "services/timers.tsx",
      },
      {
        consumers: ["services/sockets.tsx"],
        fields: ["listQueryOptions", "selectedQueryOptions"],
        route: "services/sockets.tsx",
      },
    ] as const;

    for (const entry of cases) {
      const route = readRouteSource(entry.route);
      const loader = route.split("loader:", 2)[1]?.split("component:", 1)[0];
      const consumers = entry.consumers.map(readRouteSource).join("\n");

      expect(route, entry.route).toContain("context:");
      expect(loader, entry.route).toContain("loaderArgs.context");
      expect(loader, entry.route).not.toContain("linuxio.");
      expect(consumers, entry.route).toContain("useRouteContext");
      for (const field of entry.fields) {
        expect(route, `${entry.route}: ${field}`).toContain(field);
        expect(consumers, `${entry.route}: ${field}`).toContain(field);
      }
    }
  });

  it("defaults query loaders to presence and keeps exceptions explicit", () => {
    const routeSources = sourceFiles(authenticatedRoutes)
      .filter((file) => file.endsWith(".tsx"))
      .map((file) => ({ file, source: readFileSync(file, "utf8") }))
      .filter(({ source }) => source.includes("createFileRoute("));

    const queryLoaders = routeSources.filter(({ source }) =>
      source.includes("loadRouteQueries("),
    );
    const explicitFreshness = queryLoaders
      .filter(({ source }) => source.includes("LOADER_FRESHNESS."))
      .map(({ file }) => path.relative(authenticatedRoutes, file));
    const legacyOptionObjects = queryLoaders
      .filter(({ source }) => source.includes("signal: abortController.signal"))
      .map(({ file }) => path.relative(authenticatedRoutes, file));
    const missingLoaderArgs = queryLoaders
      .filter(({ source }) => !/loadRouteQueries\(\s*loaderArgs,/.test(source))
      .map(({ file }) => path.relative(authenticatedRoutes, file));
    const missingTransportSignals = routeSources
      .filter(({ source }) => source.includes("loadRouteTransport("))
      .filter(({ source }) => !source.includes("abortController.signal"))
      .map(({ file }) => path.relative(authenticatedRoutes, file));
    const fileBrowser = queryLoaders.find(({ file }) =>
      file.endsWith(path.join("filebrowser", "$.tsx")),
    )?.source;

    expect(explicitFreshness).toEqual(["filebrowser/$.tsx"]);
    expect(fileBrowser).toContain("LOADER_FRESHNESS.BACKGROUND");
    expect(fileBrowser).not.toContain("LOADER_FRESHNESS.PRESENCE");
    expect(legacyOptionObjects).toEqual([]);
    expect(missingLoaderArgs).toEqual([]);
    expect(missingTransportSignals).toEqual([]);
  });

  it("keeps Dashboard and Hardware shells non-atomic", () => {
    const dashboardRoute = readRouteSource("index.tsx");
    const dashboardPage = readRouteSource("-dashboard/DashboardPage.tsx");
    const hardwareRoute = readRouteSource("hardware/route.tsx");
    const hardwarePage = readRouteSource(
      "hardware/-components/HardwarePage.tsx",
    );

    for (const route of [dashboardRoute, hardwareRoute]) {
      expect(route).toContain("loadRouteTransport");
      expect(route).toContain("startRouteQueryPrefetches");
      expect(route).not.toContain("configCache");
      expect(route).not.toContain("get_ui");
      expect(route).not.toContain("loadRouteQueries");
    }
    expect(dashboardRoute).toContain("linuxio.system.get_host_info");
    expect(dashboardRoute).toContain("linuxio.system.get_health_summary");
    expect(dashboardRoute).toContain("context.access.dockerAvailable === true");
    expect(dashboardRoute).not.toContain(".queryOptions(");
    expect(dashboardRoute.match(/linuxio\./g)).toHaveLength(16);
    expect(hardwareRoute).toContain("sections.sensors");
    expect(hardwareRoute).toContain("sections.systemInfo");
    expect(hardwareRoute).toContain("sections.pciDevices");
    expect(hardwareRoute).toContain("sections.memoryModules");
    expect(hardwareRoute).not.toContain(".queryOptions(");
    expect(hardwareRoute.match(/linuxio\./g)).toHaveLength(7);
    expect(dashboardPage).toContain("<Suspense");
    expect(dashboardPage).toContain("<DashboardCardSkeleton");
    expect(dashboardPage).not.toContain("WidgetLoader");
    expect(hardwarePage).toContain("<Suspense");
    expect(hardwarePage.match(/unmountOnExit/g)?.length).toBeGreaterThanOrEqual(
      5,
    );
  });

  it("keeps reconnect-sensitive handoff status in its owning dialog", () => {
    const networkRoute = readRouteSource("network/route.tsx");
    const handoffDialog = readRouteSource(
      "network/-components/BridgeHandoffDialog.tsx",
    );

    expect(networkRoute).not.toContain("get_bridge_handoff");
    expect(handoffDialog).toContain("get_bridge_handoff");
    expect(handoffDialog).toContain("refetchInterval");
    expect(handoffDialog).toContain("muxIsOpen");
  });

  it("keeps progressive hardware histories out of the route loader", () => {
    const route = readRouteSource("hardware/route.tsx");
    const cards = readRouteSource(
      "hardware/-components/HardwareHistoryCards.tsx",
    );

    expect(route).not.toContain("linuxio.monitoring.");
    for (const endpoint of [
      "get_cpu_history",
      "get_memory_history",
      "get_diskio_history",
      "get_network_history",
    ]) {
      expect(cards).toContain(`linuxio.monitoring.${endpoint}`);
    }
  });

  it("loads WireGuard network data only inside the create workflow", () => {
    const page = readRouteSource("wireguard/-components/WireguardPage.tsx");
    const createButton = readRouteSource(
      "wireguard/-components/CreateInterfaceButton.tsx",
    );

    expect(page.match(/linuxio\.wireguard\.list_interfaces/g)).toHaveLength(1);
    expect(createButton).not.toContain("linuxio.wireguard.list_interfaces");
    expect(createButton).toMatch(
      /\.\.\.linuxio\.network\.get_network_info,\s*enabled: showDialog,/,
    );
  });

  it("loads bridge options only while the create dialog is open", () => {
    const route = readRouteSource("network/route.tsx");
    const dialog = readRouteSource(
      "network/-components/CreateBridgeDialog.tsx",
    );

    expect(route).not.toContain("linuxio.network.get_bridge_options");
    expect(dialog).toMatch(
      /\.\.\.linuxio\.network\.get_bridge_options,\s*enabled: open,/,
    );
  });

  it("enables the logs service query only for status-backed filters", () => {
    const logs = readRouteSource("logs/-components/GeneralLogsPage.tsx");

    expect(logs).toMatch(
      /\.\.\.linuxio\.systemd\.list_services,\s*enabled: unitStatusNeedsServices,/,
    );
  });

  it("keeps VM child observers on the parent polling cadence", () => {
    const page = readRouteSource("vm/-components/VMPage.tsx");
    const route = readRouteSource("vm/route.tsx");
    const createDialog = readRouteSource("vm/-components/CreateVMDialog.tsx");
    const childObserverCounts = [
      ["vm/-components/VMDashboardPage.tsx", 2],
      ["vm/-components/VMImagesPage.tsx", 1],
      ["vm/-components/VMMachinesLayout.tsx", 2],
      ["vm/-components/VMNetworksPage.tsx", 1],
    ] as const;

    expect(page.match(/refetchInterval:/g)).toHaveLength(2);
    expect(page).not.toContain("linuxio.virt.networks");
    expect(route).not.toContain("linuxio.virt.networks");
    expect(createDialog).toMatch(
      /\.\.\.linuxio\.virt\.networks,\s*enabled: open,/,
    );
    expect(createDialog).toContain("refetchInterval: open ? 30000 : false");
    for (const [relativePath, observerCount] of childObserverCounts) {
      const child = readRouteSource(relativePath);
      expect(child).not.toContain("refetchInterval");
      expect(child.match(/refetchOnMount: false/g)).toHaveLength(observerCount);
    }
  });
});
