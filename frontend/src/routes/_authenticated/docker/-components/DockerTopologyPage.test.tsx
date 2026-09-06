import { act } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio, type ContainerInfo, type DockerNetwork } from "@/api";
import {
  topologyLive,
  topologyNextcloudId,
  topologyContainers,
  topologyNetworks,
} from "@/test/dockerTopologyFixture";
import {
  createTestQueryClient,
  renderWithTanStackRouter,
  screen,
  within,
} from "@/test/render";

import {
  buildTopology,
  formatHostEndpoint,
  getHostPortBindings,
  type TopologySelection,
} from "./dockerTopology";
import DockerTopologyPage from "./DockerTopologyPage";

vi.mock("@/components/docker/DockerIcon", () => ({
  default: ({ identifier }: { identifier: string }) => (
    <span data-testid={`icon-${identifier}`} />
  ),
}));

let containers: ContainerInfo[];
let networks: DockerNetwork[];

async function setup(initial: TopologySelection = {}, monitoring = true) {
  const client = createTestQueryClient();
  client.setQueryDefaults(linuxio.docker.list_containers.queryKey, {
    staleTime: Infinity,
  });
  client.setQueryDefaults(linuxio.docker.list_networks.queryKey, {
    staleTime: Infinity,
  });
  client.setQueryData(linuxio.docker.list_containers.queryKey, containers);
  client.setQueryData(linuxio.docker.list_networks.queryKey, networks);
  const liveData = topologyLive(100000);
  liveData.containers.items[0].cpu_percent = 2.5;
  vi.spyOn(linuxio.monitoring.get_live, "queryFn").mockResolvedValue(liveData);
  client.setQueryDefaults(linuxio.monitoring.get_live.queryKey, {
    staleTime: Infinity,
    queryFn: async () => liveData,
  });
  client.setQueryData(linuxio.monitoring.get_live.queryKey, liveData);
  function Subject() {
    const [selection, setSelection] = useState(initial);
    return (
      <DockerTopologyPage
        onSelectionChange={setSelection}
        selection={selection}
      />
    );
  }
  const result = renderWithTanStackRouter(<Subject />, {
    queryClient: client,
    capabilities: { monitoringAvailable: monitoring },
  });
  await screen.findByRole("heading", { name: "Docker topology" });
  return { client, ...result };
}

beforeEach(() => {
  containers = structuredClone(topologyContainers);
  networks = structuredClone(topologyNetworks);
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("Docker topology", () => {
  it("inspects attachments, published ports and measured activity", async () => {
    const { user } = await setup();
    expect(screen.getByTestId("icon-nextcloud")).toBeInTheDocument();
    const ports = screen.getByRole("list", { name: "Host port bindings" });
    expect(within(ports).getAllByRole("listitem")).toHaveLength(2);
    expect(within(ports).getByText("*:8080/tcp → 80/tcp")).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Inspect container nextcloud" }),
    );
    const details = screen.getByRole("region", { name: "Topology details" });
    expect(within(details).getByText("172.20.0.2/16")).toBeInTheDocument();
    expect(within(details).getByText("172.21.0.2/16")).toBeInTheDocument();
    expect(within(details).getByText("2.5%")).toBeInTheDocument();
    expect(within(details).getByText("128 MB")).toBeInTheDocument();
    expect(within(details).getByText("18 kB/s")).toBeInTheDocument();
    expect(within(details).getByText("80 kB/s")).toBeInTheDocument();
    const node = screen.getByRole("button", {
      name: "Inspect container nextcloud",
    });
    expect(within(node).getByText("18 kB/s")).toBeInTheDocument();
    expect(within(node).getByText("80 kB/s")).toBeInTheDocument();
    expect(
      within(details).getByRole("link", { name: "Open container details" }),
    ).toHaveAttribute(
      "href",
      expect.stringContaining(`container=${topologyNextcloudId}`),
    );
    expect(
      document.querySelectorAll('path[data-highlighted="true"]'),
    ).toHaveLength(2);
    await user.click(
      within(details).getByRole("button", { name: "cloud_internal" }),
    );
    expect(
      within(details).getByRole("heading", { name: "cloud_internal" }),
    ).toBeInTheDocument();
    expect(
      document.querySelectorAll('path[data-highlighted="true"]'),
    ).toHaveLength(3);
  });

  it("folds Compose groups without losing their connections or the inspector", async () => {
    const { user } = await setup({ container: topologyNextcloudId });
    const paths = [
      ...document.querySelectorAll("path.docker-topology__edge"),
    ].map((path) => path.getAttribute("d"));
    await user.click(
      screen.getByRole("button", { name: "Collapse stack cloud" }),
    );
    expect(
      screen.queryByRole("button", { name: "Inspect container nextcloud" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "Topology details" }),
    ).toHaveTextContent("nextcloud:31");
    expect(
      document.querySelectorAll("path.docker-topology__edge"),
    ).toHaveLength(paths.length);
    await user.click(
      screen.getByRole("button", { name: "Expand stack cloud" }),
    );
    expect(
      [...document.querySelectorAll("path.docker-topology__edge")].map((path) =>
        path.getAttribute("d"),
      ),
    ).toEqual(paths);
  });

  it("keeps disconnected and stopped containers visible without invented activity", async () => {
    networks = [];
    const { user } = await setup();
    await user.click(
      screen.getByRole("button", { name: "Inspect container backup" }),
    );
    const details = screen.getByRole("region", { name: "Topology details" });
    expect(within(details).getByText("No listed network")).toBeInTheDocument();
    expect(within(details).getAllByText("Unavailable")).toHaveLength(2);
    expect(
      document.querySelectorAll("path.docker-topology__edge"),
    ).toHaveLength(0);
  });

  it("offers recovery for empty inventory and a deleted selection", async () => {
    containers = [];
    networks = [];
    await setup({ container: "deleted" });
    expect(screen.getByText(/No containers yet/)).toBeInTheDocument();
    expect(
      screen.getByText(/This resource is no longer listed/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Clear selection" }),
    ).toBeInTheDocument();
  });

  it("expires repeated samples and recovers when a new sample arrives", async () => {
    const { client } = await setup();
    vi.useFakeTimers();
    const repeatedSample = topologyLive(100001);
    vi.mocked(linuxio.monitoring.get_live.queryFn).mockResolvedValue(
      repeatedSample,
    );
    await act(async () => {
      client.setQueryData(linuxio.monitoring.get_live.queryKey, repeatedSample);
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByText("Live activity")).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(16000));
    expect(screen.getByText("Metrics unavailable")).toBeInTheDocument();
    expect(
      document.querySelectorAll('.docker-topology__rate[data-active="true"]'),
    ).toHaveLength(0);
    await act(async () => {
      client.setQueryData(linuxio.monitoring.get_live.queryKey, {
        captured_at_ms: 120000,
        containers: { captured_at_ms: 120000, items: [] },
      });
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByText("Live activity")).toBeInTheDocument();
    await act(async () => {
      client.setQueryData(linuxio.monitoring.get_live.queryKey, {
        captured_at_ms: 160000,
        containers: { captured_at_ms: 120000, items: [] },
      });
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByText("Metrics unavailable")).toBeInTheDocument();
  });

  it("works without monitoring and allows pausing animation", async () => {
    const { user } = await setup({}, false);
    expect(screen.getByText("Metrics unavailable")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Pause animation" }));
    expect(
      screen.getByRole("button", { name: "Resume animation" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(document.querySelector(".docker-topology-page")).toHaveAttribute(
      "data-paused",
      "true",
    );
  });
});

describe("topology inventory", () => {
  it("keeps unknown endpoints and stable positions across reordered inventory", () => {
    networks[0].Containers!["unknown"] = { Name: "pending-container" };
    const first = buildTopology(containers, networks, new Set());
    const reordered = buildTopology(
      [...containers].reverse(),
      [...networks].reverse(),
      new Set(),
    );
    expect(first).toEqual(reordered);
    expect(first.inventory.get("unknown")?.State).toBe("Unknown");
    expect(first.edges.some((edge) => edge.container === "unknown")).toBe(true);
  });

  it("preserves address and protocol distinctions while deduplicating wildcards", () => {
    containers[0].Ports!.push(
      { IP: "2001:db8::1", PublicPort: 8080, PrivatePort: 80, Type: "tcp" },
      { IP: "127.0.0.1", PublicPort: 8080, PrivatePort: 80, Type: "udp" },
      { PrivatePort: 443, Type: "tcp" },
    );
    expect(
      getHostPortBindings([containers[0]]).map(formatHostEndpoint),
    ).toEqual(["*:8080/tcp", "[2001:db8::1]:8080/tcp", "127.0.0.1:8080/udp"]);
  });
});
