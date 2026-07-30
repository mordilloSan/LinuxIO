import { readFileSync } from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const authenticatedRoutes = path.resolve(
  process.cwd(),
  "src/routes/_authenticated",
);
const readRouteSource = (relativePath: string) =>
  readFileSync(path.join(authenticatedRoutes, relativePath), "utf8");

describe("targeted route query ownership", () => {
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
      expect(cards).toContain(`linuxio.monitoring.${endpoint}.queryOptions`);
    }
  });

  it("loads WireGuard network data only inside the create workflow", () => {
    const page = readRouteSource("wireguard/-components/WireguardPage.tsx");
    const createButton = readRouteSource(
      "wireguard/-components/CreateInterfaceButton.tsx",
    );

    expect(page.match(/list_interfaces\.queryOptions/g)).toHaveLength(1);
    expect(createButton).not.toContain("list_interfaces.queryOptions");
    expect(createButton).toMatch(
      /get_network_info\.queryOptions\(\{\s*enabled: showDialog,/,
    );
  });

  it("enables the logs service query only for status-backed filters", () => {
    const logs = readRouteSource("logs/-components/GeneralLogsPage.tsx");

    expect(logs).toMatch(
      /list_services\.queryOptions\(\{\s*enabled: unitStatusNeedsServices,/,
    );
  });

  it("keeps VM child observers on the parent polling cadence", () => {
    const page = readRouteSource("vm/-components/VMPage.tsx");
    const childObserverCounts = [
      ["vm/-components/VMDashboardPage.tsx", 2],
      ["vm/-components/VMImagesPage.tsx", 1],
      ["vm/-components/VMMachinesLayout.tsx", 2],
      ["vm/-components/VMNetworksPage.tsx", 1],
    ] as const;

    expect(page.match(/refetchInterval:/g)).toHaveLength(2);
    for (const [relativePath, observerCount] of childObserverCounts) {
      const child = readRouteSource(relativePath);
      expect(child).not.toContain("refetchInterval");
      expect(child.match(/refetchOnMount: false/g)).toHaveLength(observerCount);
    }
  });
});
