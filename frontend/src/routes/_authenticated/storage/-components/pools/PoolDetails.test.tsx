import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithTanStackRouter, screen, within } from "@/test/render";

import PoolDetails from "./PoolDetails";
import { poolFixture } from "./poolTestFixtures";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
  });
  const mocked = {
    ...actual,
    useCallMutation,
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_list: {
          queryKey: ["test-btrfs-list"],
          queryFn: () => mocks.list(),
        },
        btrfs_snapshots: (request: { uuid: string }) => ({
          queryKey: ["test-snapshots", request.uuid],
          queryFn: () => Promise.resolve([]),
        }),
        btrfs_replication_jobs: {
          queryKey: ["test-replication-jobs"],
          queryFn: () => Promise.resolve([]),
        },
        get_btrfs_maintenance: {
          queryKey: ["test-maintenance"],
          queryFn: () => Promise.resolve({ tasks: [] }),
        },
      },
    },
  };
  return {
    ...mocked,
    linuxio: mockMutationHooks(mocked.linuxio, useCallMutation),
  };
});

const route = (name: string) => {
  const mutate = mocks.mutations[`storage.${name}`];
  if (!mutate) throw new Error(`no mutation registered for ${name}`);
  return mutate;
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.list.mockResolvedValue([poolFixture()]);
});

const renderDetails = (tab: "overview" | "disks" = "overview") => {
  const onTab = vi.fn();
  const onGone = vi.fn();
  renderWithTanStackRouter(
    <PoolDetails onGone={onGone} onTab={onTab} tab={tab} uuid="uuid-tank" />,
    { auth: { privileged: true }, capabilities: { btrfsAvailable: true } },
  );
  return { onGone, onTab };
};

describe("PoolDetails", () => {
  it("leads with the pool's health and opens a tab", async () => {
    const user = userEvent.setup();
    const { onTab } = renderDetails();

    expect(
      await screen.findByRole("region", { name: "Protection" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Snapshots" }));
    expect(onTab).toHaveBeenCalledWith("snapshots");
  });

  it("shows the disks on the Disks tab", async () => {
    renderDetails("disks");
    expect(await screen.findByText("/dev/sda")).toBeInTheDocument();
    expect(screen.getByText("Space")).toBeInTheDocument();
  });

  it("renames the pool from its menu", async () => {
    const user = userEvent.setup();
    renderDetails();
    await user.click(
      await screen.findByRole("button", { name: "More pool actions" }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Rename…" }));
    const dialog = await screen.findByRole("dialog");
    const name = within(dialog).getByRole("textbox", { name: "Name" });
    expect(name).toHaveValue("tank");
    await user.clear(name);
    await user.type(name, "media");
    await user.click(within(dialog).getByRole("button", { name: "Rename" }));

    expect(route("btrfs_set_label")).toHaveBeenCalledWith(
      { uuid: "uuid-tank", label: "media" },
      expect.anything(),
    );
  });

  it("destroys the pool from its menu, naming its disks", async () => {
    const user = userEvent.setup();
    renderDetails();
    await user.click(
      await screen.findByRole("button", { name: "More pool actions" }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Destroy…" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("/dev/sda, /dev/sdb");
    expect(dialog).not.toHaveTextContent("fstab");
    await user.click(within(dialog).getByRole("button", { name: "Destroy" }));

    expect(route("btrfs_pool_destroy")).toHaveBeenCalledWith({
      uuid: "uuid-tank",
    });
  });
});
