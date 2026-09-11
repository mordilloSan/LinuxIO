import { act, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";
import { topologyNextcloudId } from "@/test/dockerTopologyFixture";
import {
  createTestQueryClient,
  renderWithTanStackRouter,
  screen,
  within,
} from "@/test/render";
import {
  storageBlock,
  storageContainers,
  storageFilesystems,
  storageGroups,
  storageInventory,
  storageLive,
  storageVMs,
} from "@/test/storageTopologyFixture";

import {
  buildStorageTopology,
  findStorageMount,
  relatedStorageNodes,
  type StorageSelection,
} from "./storageTopology";
import StorageTopologyPage from "./StorageTopologyPage";

vi.mock("@/components/docker/DockerIcon", () => ({
  default: ({ identifier }: { identifier: string }) => (
    <span data-testid={`icon-${identifier}`} />
  ),
}));

async function setup(
  initial: StorageSelection = {},
  available = true,
  inventory = storageInventory,
) {
  const client = createTestQueryClient();
  vi.spyOn(linuxio.storage.get_topology, "queryFn").mockResolvedValue(
    inventory,
  );
  vi.spyOn(linuxio.storage.list_vgs, "queryFn").mockResolvedValue(
    storageGroups,
  );
  vi.spyOn(linuxio.docker.list_containers, "queryFn").mockResolvedValue(
    storageContainers,
  );
  vi.spyOn(linuxio.virt.list, "queryFn").mockResolvedValue(storageVMs);
  vi.spyOn(linuxio.monitoring.get_live, "queryFn").mockResolvedValue(
    storageLive(100000),
  );
  client.setQueryData(linuxio.storage.get_topology.queryKey, inventory);
  client.setQueryData(linuxio.storage.list_vgs.queryKey, storageGroups);
  client.setQueryData(
    linuxio.docker.list_containers.queryKey,
    storageContainers,
  );
  client.setQueryData(linuxio.virt.list.queryKey, storageVMs);
  client.setQueryData(
    linuxio.monitoring.get_live.queryKey,
    storageLive(100000),
  );
  function Subject() {
    const [selection, setSelection] = useState(initial);
    return (
      <StorageTopologyPage
        selection={selection}
        onSelectionChange={setSelection}
      />
    );
  }
  const result = renderWithTanStackRouter(<Subject />, {
    queryClient: client,
    capabilities: {
      monitoringAvailable: available,
      dockerAvailable: available,
      libvirtAvailable: available,
    },
  });
  await screen.findByRole("heading", { name: "Storage topology" });
  return { client, ...result };
}
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("storage topology", () => {
  it("traces disk, partition, LVM, mount, and application relationships", () => {
    const topology = buildStorageTopology(
      storageInventory,
      storageFilesystems,
      storageGroups,
      storageContainers,
      storageVMs,
    );
    const mount = topology.inventory.get("mount:/srv/appdata")!;
    expect(mount.group?.name).toBe("system");
    expect(mount.backing.map((block) => block.path)).toEqual([
      "/dev/nvme0n1",
      "/dev/nvme0n1p2",
      "/dev/mapper/system-apps",
    ]);
    const related = relatedStorageNodes(
      "container:jellyfin-id",
      topology.edges,
    );
    expect(related).toEqual(
      new Set([
        "container:jellyfin-id",
        "mount:/srv/appdata",
        "mount:/srv/media",
        "drive:/dev/nvme0n1",
        "drive:/dev/sda",
      ]),
    );
    expect(related.has(`container:${topologyNextcloudId}`)).toBe(false);
    expect(topology.edges.some((edge) => edge.to === "mount:/mnt/backup")).toBe(
      false,
    );
    expect(topology.inventory.has("drive:/dev/sdb")).toBe(true);
    expect(topology.inventory.has("mount:/run")).toBe(false);
    const reordered = buildStorageTopology(
      {
        devices: [...storageInventory.devices].reverse(),
        mounts: [...storageInventory.mounts].reverse(),
      },
      storageFilesystems,
      storageGroups,
      [...storageContainers].reverse(),
      storageVMs,
    );
    expect(reordered.nodes.map(({ id, top }) => ({ id, top }))).toEqual(
      topology.nodes.map(({ id, top }) => ({ id, top })),
    );
  });

  it("preserves multiple backing disks, unmounted volumes, and raw VM disks", () => {
    const inventory = structuredClone(storageInventory);
    inventory.devices.push(
      storageBlock(
        "/dev/md0",
        "raid1",
        1024,
        ["/dev/sda", "/dev/sdb"],
        ["/raid"],
      ),
    );
    inventory.devices.push(storageBlock("/dev/sdb2", "part", 8, ["/dev/sdb"]));
    inventory.mounts.push({
      device: "/dev/md0",
      path: "/raid",
      fsType: "ext4",
      readOnly: false,
    });
    const vms = [
      {
        ...storageVMs[0],
        disks: [
          { device: "disk", target: "vdb", path: "/dev/sdb2", owned: false },
        ],
      },
    ];
    const topology = buildStorageTopology(inventory, [], [], [], vms);
    expect(
      topology.edges
        .filter((edge) => edge.to === "mount:/raid")
        .map((edge) => edge.from)
        .sort(),
    ).toEqual(["drive:/dev/sda", "drive:/dev/sdb"]);
    expect(topology.inventory.get("volume:/dev/sdb2")?.block?.sizeBytes).toBe(
      8 * 1024 ** 3,
    );
    expect(relatedStorageNodes("vm:home-assistant", topology.edges)).toEqual(
      new Set(["vm:home-assistant", "volume:/dev/sdb2", "drive:/dev/sdb"]),
    );
  });

  it("respects nested mount boundaries and keeps unrecognized sources visible", () => {
    expect(findStorageMount("/database/file", ["/", "/data"])).toBe("/");
    expect(findStorageMount("/data/../database/file", ["/", "/data"])).toBe(
      "/",
    );
    expect(findStorageMount("/data/cache/file", ["/data", "/data/cache"])).toBe(
      "/data/cache",
    );
    expect(findStorageMount("relative/file", ["/"])).toBeUndefined();
    const inventory = structuredClone(storageInventory);
    inventory.mounts.push({
      path: "/srv/appdata/cache",
      device: "tmpfs",
      fsType: "tmpfs",
      readOnly: false,
    });
    const containers = structuredClone(storageContainers);
    containers[0].Mounts![0].Source = "/srv/appdata/cache/config";
    const topology = buildStorageTopology(inventory, [], [], containers, []);
    const related = relatedStorageNodes(
      `container:${containers[0].Id}`,
      topology.edges,
    );
    expect(related).toEqual(
      new Set([`container:${containers[0].Id}`, "mount:/srv/appdata/cache"]),
    );
    const missing = buildStorageTopology(
      { devices: [], mounts: [] },
      [],
      [],
      containers,
      [],
    );
    expect(missing.edges).toHaveLength(0);
    expect(
      missing.inventory.get(`container:${containers[0].Id}`)?.uses?.[0].source,
    ).toBe("/srv/appdata/cache/config");
  });

  it("inspects storage paths, capacities, and existing detail links", async () => {
    const { user } = await setup();
    await user.click(
      screen.getByRole("button", { name: "Inspect mount point /srv/appdata" }),
    );
    const details = screen.getByRole("complementary", {
      name: "Storage details",
    });
    expect(
      within(details).getByText("system", { exact: true }),
    ).toBeInTheDocument();
    expect(
      within(details).getByText("/dev/nvme0n1p2", { exact: true }),
    ).toBeInTheDocument();
    expect(
      within(details).getByRole("link", { name: "Open filesystem details" }),
    ).toHaveAttribute("href", expect.stringContaining("fs="));
    expect(
      document.querySelectorAll(
        '.storage-topology__edge[data-highlighted="true"]',
      ),
    ).toHaveLength(5);
    await user.click(within(details).getByRole("button", { name: "jellyfin" }));
    expect(
      within(details).getByText("→ /media · Read only"),
    ).toBeInTheDocument();
    expect(
      within(details).getByRole("link", { name: "Open container details" }),
    ).toHaveAttribute("href", expect.stringContaining("container=jellyfin-id"));
    await user.click(screen.getByRole("button", { name: "Clear selection" }));
    expect(
      within(details).getByText("Your storage, connected"),
    ).toBeInTheDocument();
  });

  it("keeps block and mount inventory available without optional services", async () => {
    const { user } = await setup({}, false);
    expect(screen.getByText("Metrics unavailable")).toBeInTheDocument();
    expect(document.querySelectorAll(".app-topology-edge__grain")).toHaveLength(
      0,
    );
    expect(
      screen.getByRole("button", { name: "Inspect mount point /srv/appdata" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Inspect container jellyfin" }),
    ).not.toBeInTheDocument();
    expect(linuxio.monitoring.get_live.queryFn).not.toHaveBeenCalled();
    expect(linuxio.docker.list_containers.queryFn).not.toHaveBeenCalled();
    expect(linuxio.virt.list.queryFn).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Pause animation" }));
    expect(
      screen.getByRole("button", { name: "Resume animation" }),
    ).toHaveAttribute("aria-pressed", "true");
  });

  it("recovers from empty inventory and a removed URL selection", async () => {
    const { user } = await setup({ node: "mount:/gone" }, false, {
      devices: [],
      mounts: [],
    });
    expect(
      screen.getByRole("heading", { name: "No storage devices found" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Item no longer available" }),
    ).toBeInTheDocument();
    await user.keyboard("{Escape}");
    expect(
      screen.getByRole("heading", { name: "Your storage, connected" }),
    ).toBeInTheDocument();
  });

  it("expires unchanged measurements and recovers when samples advance", async () => {
    const { client } = await setup();
    expect(
      document.querySelectorAll(".app-topology-edge__grain").length,
    ).toBeGreaterThan(0);
    vi.useFakeTimers();
    const sample = storageLive(120000);
    vi.mocked(linuxio.monitoring.get_live.queryFn).mockResolvedValue(sample);
    await act(async () => {
      client.setQueryData(linuxio.monitoring.get_live.queryKey, sample);
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByText("Live activity")).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(16000);
    });
    expect(screen.getByText("Metrics unavailable")).toBeInTheDocument();
    expect(
      document.querySelectorAll('.storage-topology__rate[data-active="true"]'),
    ).toHaveLength(0);
    expect(document.querySelectorAll(".app-topology-edge__grain")).toHaveLength(
      0,
    );
    await act(async () => {
      client.setQueryData(
        linuxio.monitoring.get_live.queryKey,
        storageLive(140000),
      );
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByText("Live activity")).toBeInTheDocument();
    expect(
      document.querySelectorAll(".app-topology-edge__grain").length,
    ).toBeGreaterThan(0);
  });

  it("uses measured directions without attributing disk activity to applications", async () => {
    const { client } = await setup();
    // Four device links and three writable container links; the read-only
    // media bind, stopped container, and unmeasured VM have no write grains.
    expect(
      document.querySelectorAll(
        '.app-topology-edge__flow[data-direction="reverse"]',
      ),
    ).toHaveLength(7);
    const sample = storageLive(120000);
    sample.disks.nvme0n1.read_bytes_per_sec = 0;
    sample.disks.sda.read_bytes_per_sec = 0;
    sample.disks.sda.write_bytes_per_sec = 0;
    sample.containers.items = sample.containers.items.slice(0, 1);
    sample.containers.items[0].block_write_bytes_per_sec = 0;
    await act(async () => {
      client.setQueryData(linuxio.monitoring.get_live.queryKey, sample);
    });
    await waitFor(() =>
      expect(
        document.querySelectorAll(
          '.app-topology-edge__flow[data-direction="forward"]',
        ),
      ).toHaveLength(1),
    );
    expect(
      document.querySelectorAll(
        '.app-topology-edge__flow[data-direction="reverse"]',
      ),
    ).toHaveLength(3);
    await act(async () => {
      client.setQueryData(linuxio.monitoring.get_live.queryKey, {
        ...sample,
        captured_at_ms: 140000,
      });
    });
    await waitFor(() =>
      expect(
        document.querySelectorAll(
          '.app-topology-edge__flow[data-direction="forward"]',
        ),
      ).toHaveLength(0),
    );
    expect(
      document.querySelectorAll(
        '.app-topology-edge__flow[data-direction="reverse"]',
      ),
    ).toHaveLength(3);
  });
});
