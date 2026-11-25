import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsOfflinePool, BtrfsReplicationJob, MDArray } from "@/api";
import { renderWithTanStackRouter, screen, within } from "@/test/render";

import { poolFixture, singlePool } from "../pools/poolTestFixtures";
import StorageOverview from "./StorageOverview";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  offline: vi.fn(),
  jobs: vi.fn(),
  md: vi.fn(),
  samba: vi.fn(),
  nfs: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
  });
  const query = (key: string, fn: () => unknown) => ({
    queryKey: [key],
    queryFn: fn,
  });
  const mocked = {
    ...actual,
    useCallMutation,
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_list: query("test-btrfs-list", () => mocks.list()),
        btrfs_pools_offline: query("test-offline", () => mocks.offline()),
        btrfs_replication_jobs: query("test-jobs", () => mocks.jobs()),
        md_list: query("test-md", () => mocks.md()),
        get_topology: query("test-topology", () =>
          Promise.resolve({ devices: [], mounts: [] }),
        ),
      },
      shares: {
        ...actual.linuxio.shares,
        list_samba_shares: query("test-samba", () => mocks.samba()),
        list_nfs_shares: query("test-nfs", () => mocks.nfs()),
      },
    },
  };
  return {
    ...mocked,
    linuxio: mockMutationHooks(mocked.linuxio, useCallMutation),
  };
});

const copied: BtrfsReplicationJob = {
  id: "job-1",
  uuid: "uuid-tank",
  path: "@",
  target: { kind: "ssh", path: "/backup", host: "nas2" },
  keepOnTarget: 0,
  enabled: true,
  after: "timeline",
};

const cold: BtrfsOfflinePool = {
  uuid: "uuid-cold",
  label: "cold",
  numDevices: 2,
  devices: ["/dev/sdb"],
  totalBytes: 0,
  mountpoint: "/mnt/cold",
  missing: 1,
};

const md = (degraded: number): MDArray => ({
  name: "md127",
  path: "/dev/md127",
  level: "raid1",
  state: "clean",
  metadataVersion: "1.2",
  sizeBytes: 0,
  raidDisks: 2,
  degraded,
  syncAction: "idle",
  syncProgress: 0,
  syncSpeedBytes: 0,
  mismatchCount: 0,
  members: [],
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.list.mockResolvedValue([poolFixture()]);
  mocks.offline.mockResolvedValue([]);
  mocks.jobs.mockResolvedValue([copied]);
  mocks.md.mockResolvedValue([]);
  mocks.samba.mockResolvedValue([
    { name: "media", properties: { path: "/mnt/tank/media" } },
  ]);
  mocks.nfs.mockResolvedValue([
    { path: "/mnt/tank/media", active: true, clients: [] },
  ]);
});

const admin = {
  auth: { privileged: true },
  capabilities: { btrfsAvailable: true },
};

describe("StorageOverview", () => {
  it("says when everything is healthy, once it has read", async () => {
    renderWithTanStackRouter(<StorageOverview />, admin);
    await screen.findByRole("button", { name: "Open tank details" });
    expect(
      await screen.findByText("All pools and arrays are healthy"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Pools are also checked every ten minutes; their problems go to the bell.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "Worth a look" }),
    ).not.toBeInTheDocument();
  });

  it("lists what is worth a look, each with where it is fixed", async () => {
    mocks.list.mockResolvedValue([poolFixture({ degraded: true }), singlePool]);
    mocks.offline.mockResolvedValue([cold]);
    mocks.md.mockResolvedValue([md(1)]);
    renderWithTanStackRouter(<StorageOverview />, admin);

    const look = await screen.findByRole("region", { name: "Worth a look" });
    // The degraded pool, the pool not mounted and the array are problems; one
    // disk and no copy elsewhere are advice.
    expect(
      await screen.findByText("3 problems need a look"),
    ).toBeInTheDocument();
    expect(within(look).getByText("tank: Degraded")).toBeInTheDocument();
    expect(
      within(look).getByText(
        "Storage pool 2 has one disk: a disk failure loses it",
      ),
    ).toBeInTheDocument();
    expect(
      within(look).getByText("Storage pool 2 is not copied anywhere else"),
    ).toBeInTheDocument();
    expect(within(look).getByText("cold is not mounted")).toBeInTheDocument();
    expect(
      within(look).getByText("md127 is running without a disk"),
    ).toBeInTheDocument();
    expect(
      within(look).getByRole("link", { name: "tank: Degraded" }),
    ).toHaveAttribute("href", "/storage/pools/uuid-tank");
  });

  it("reads healthy with only advice to give", async () => {
    mocks.list.mockResolvedValue([singlePool]);
    mocks.jobs.mockResolvedValue([]);
    renderWithTanStackRouter(<StorageOverview />, admin);
    const look = await screen.findByRole("region", { name: "Worth a look" });
    expect(
      within(look).getByText(
        "Storage pool 1 has one disk: a disk failure loses it",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText("All pools and arrays are healthy"),
    ).toBeInTheDocument();
  });

  it("says it is checking while the lists load", async () => {
    mocks.list.mockReturnValue(new Promise(() => {}));
    renderWithTanStackRouter(<StorageOverview />, admin);
    expect(await screen.findByText("Checking…")).toBeInTheDocument();
    expect(
      screen.queryByText("All pools and arrays are healthy"),
    ).not.toBeInTheDocument();
  });

  it("says what could not be read", async () => {
    mocks.md.mockRejectedValue(new Error("boom"));
    renderWithTanStackRouter(<StorageOverview />, admin);
    expect(
      await screen.findByText("Could not read the md arrays"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("All pools and arrays are healthy"),
    ).not.toBeInTheDocument();
  });

  it("joins a folder shared over SMB and NFS whatever its trailing slash", async () => {
    mocks.samba.mockResolvedValue([
      { name: "media", properties: { path: "/mnt/tank/media/" } },
    ]);
    renderWithTanStackRouter(<StorageOverview />, admin);
    const shared = await screen.findByRole("region", {
      name: "Shared folders",
    });
    await within(shared).findByText("/mnt/tank/media");
    expect(within(shared).getAllByRole("listitem")).toHaveLength(1);
  });

  it("shows the pools and opens one", async () => {
    const user = userEvent.setup();
    const { router } = renderWithTanStackRouter(<StorageOverview />, admin);
    await user.click(
      await screen.findByRole("button", { name: "Open tank details" }),
    );
    expect(router.state.location.pathname).toBe("/storage/pools/uuid-tank");
  });

  it("lists shared folders with the protocols they use", async () => {
    renderWithTanStackRouter(<StorageOverview />, admin);
    const shared = await screen.findByRole("region", {
      name: "Shared folders",
    });
    expect(
      await within(shared).findByText("/mnt/tank/media"),
    ).toBeInTheDocument();
    expect(within(shared).getByText("SMB")).toBeInTheDocument();
    expect(within(shared).getByText("NFS")).toBeInTheDocument();
  });

  it("opens Create pool", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(<StorageOverview />, admin);
    await user.click(
      await screen.findByRole("button", { name: "Create pool" }),
    );
    expect(
      await screen.findByRole("dialog", { name: "Create pool" }),
    ).toBeInTheDocument();
  });

  it("leaves pools out for a session that cannot open them", async () => {
    renderWithTanStackRouter(<StorageOverview />, {
      auth: { privileged: false },
      capabilities: { btrfsAvailable: true },
    });
    expect(
      await screen.findByText(
        "Pools need the btrfs tools and a session with administrator rights.",
      ),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("All arrays are healthy"),
    ).toBeInTheDocument();
    expect(mocks.list).not.toHaveBeenCalled();
  });
});
