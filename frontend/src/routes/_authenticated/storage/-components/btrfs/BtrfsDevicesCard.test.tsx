import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  BtrfsFilesystem,
  StorageBlockDevice,
  StorageTopologyInventory,
} from "@/api";
import { render } from "@/test/render";
import type { TaskStreamActionMockConfig } from "@/test/taskStreamAction";

import BtrfsDevicesCard from "./BtrfsDevicesCard";
import { BtrfsHealthTile } from "./BtrfsOverviewTiles";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
  pending: new Set<string>(),
  submit: vi.fn(),
  topology: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { createTaskStreamActionMock } =
    await import("@/test/taskStreamAction");
  const task = (name: string) => ({
    useTaskStreamAction: (config?: TaskStreamActionMockConfig) => ({
      isPending: mocks.pending.has(name),
      ...createTaskStreamActionMock(
        {
          openStream: () => ({}),
          runStream: () => Promise.resolve(undefined),
          submit: (request) => mocks.submit(name, request),
        },
        config,
      ),
    }),
  });
  return {
    ...actual,
    call: (route: string) =>
      route === "tasks.list" ? Promise.resolve([]) : Promise.reject(),
    useCallMutation: (endpoint: { route: string }) => ({
      isPending: false,
      mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
    }),
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        get_topology: {
          queryKey: ["test-get-topology"],
          queryFn: () => mocks.topology(),
        },
        btrfs_balance: task("btrfs_balance"),
        btrfs_device_remove: task("btrfs_device_remove"),
        btrfs_device_replace: task("btrfs_device_replace"),
        btrfs_scrub_start: task("btrfs_scrub_start"),
      },
    },
  };
});

const noErrors = { write: 0, read: 0, flush: 0, corruption: 0, generation: 0 };
const GiB = 1024 ** 3;

const filesystem = (deviceCount: number): BtrfsFilesystem => ({
  uuid: "uuid-1",
  label: "data",
  mountpoints: [],
  totalBytes: 100 * GiB,
  unallocatedBytes: 40 * GiB,
  usedBytes: 30 * GiB,
  allocation: [
    { kind: "data", profile: "single", totalBytes: GiB, usedBytes: GiB },
    { kind: "metadata", profile: "dup", totalBytes: GiB, usedBytes: GiB },
  ],
  devices: Array.from({ length: deviceCount }, (_, index) => ({
    id: index + 1,
    path: `/dev/sd${String.fromCharCode(97 + index)}`,
    sizeBytes: 100 * GiB,
    usedBytes: 10 * GiB,
    missing: false,
    errors: noErrors,
  })),
  scrub: { status: "none", errorCount: 0, bytesScrubbed: 0, percent: 0 },
  degraded: false,
});

const blockDevice = (
  overrides: Partial<StorageBlockDevice>,
): StorageBlockDevice => ({
  path: "/dev/sdx",
  name: "sdx",
  kernelName: "sdx",
  type: "disk",
  sizeBytes: 50 * GiB,
  fsType: "",
  mountpoints: [],
  parents: [],
  model: "Spare",
  serial: "",
  transport: "sata",
  readOnly: false,
  ...overrides,
});

const topology = (devices: StorageBlockDevice[]) =>
  ({ devices, mounts: [] }) as unknown as StorageTopologyInventory;

const route = (name: string) => {
  const mutate = mocks.mutations[`storage.${name}`];
  if (!mutate) throw new Error(`no mutation registered for ${name}`);
  return mutate;
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.pending.clear();
  mocks.submit.mockResolvedValue({ id: "task-1" });
  mocks.topology.mockResolvedValue(
    topology([
      blockDevice({}),
      blockDevice({ path: "/dev/sdy", name: "sdy", fsType: "ext4" }),
      blockDevice({ path: "/dev/sdz", name: "sdz", mountpoints: ["/mnt"] }),
      blockDevice({ path: "/dev/sdw", name: "sdw" }),
      blockDevice({
        path: "/dev/sdw1",
        name: "sdw1",
        type: "part",
        parents: ["/dev/sdw"],
        mountpoints: ["/boot"],
      }),
    ]),
  );
});

const openBalance = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(screen.getByRole("button", { name: "Change profile" }));
  return screen.findByRole("dialog");
};

const pick = async (
  user: ReturnType<typeof userEvent.setup>,
  dialog: HTMLElement,
  select: string,
  option: string,
) => {
  await user.click(within(dialog).getByRole("combobox", { name: select }));
  await user.click(await screen.findByRole("option", { name: option }));
};

describe("BtrfsDevicesCard actions", () => {
  it("offers raid10 only with at least four devices", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<BtrfsDevicesCard filesystem={filesystem(2)} />);
    const dialog = await openBalance(user);
    await user.click(
      within(dialog).getByRole("combobox", { name: "Data profile" }),
    );
    expect(screen.getByRole("option", { name: "raid1" })).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "raid10" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "raid5" }),
    ).not.toBeInTheDocument();
    unmount();

    render(<BtrfsDevicesCard filesystem={filesystem(4)} />);
    const wide = await openBalance(user);
    await user.click(
      within(wide).getByRole("combobox", { name: "Data profile" }),
    );
    expect(screen.getByRole("option", { name: "raid10" })).toBeInTheDocument();
  });

  it("submits the chosen balance profiles", async () => {
    const user = userEvent.setup();
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    const dialog = await openBalance(user);
    await pick(user, dialog, "Data profile", "raid1");
    await pick(user, dialog, "Metadata profile", "raid1");
    await user.click(within(dialog).getByRole("button", { name: "Balance" }));

    expect(mocks.submit).toHaveBeenCalledWith("btrfs_balance", {
      uuid: "uuid-1",
      dataProfile: "raid1",
      metadataProfile: "raid1",
    });
  });

  it("omits a profile left on keep current", async () => {
    const user = userEvent.setup();
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    const dialog = await openBalance(user);
    await pick(user, dialog, "Data profile", "raid1");
    await user.click(within(dialog).getByRole("button", { name: "Balance" }));

    expect(mocks.submit.mock.calls[0][1]).toEqual({
      uuid: "uuid-1",
      dataProfile: "raid1",
    });
    expect(mocks.submit.mock.calls[0][1]).not.toHaveProperty("metadataProfile");
  });

  it("does not count missing devices toward profile availability", async () => {
    const user = userEvent.setup();
    const degraded = filesystem(4);
    degraded.devices[3].missing = true;
    render(<BtrfsDevicesCard filesystem={degraded} />);

    const dialog = await openBalance(user);
    await user.click(
      within(dialog).getByRole("combobox", { name: "Data profile" }),
    );
    expect(screen.getByRole("option", { name: "raid1c3" })).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "raid10" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "raid1c4" }),
    ).not.toBeInTheDocument();
  });

  it("shows progress and locks the actions while a balance runs", async () => {
    const user = userEvent.setup();
    mocks.pending.add("btrfs_balance");
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    expect(screen.getAllByRole("progressbar").length).toBeGreaterThan(0);
    expect(screen.getByText("Working...")).toBeInTheDocument();
    for (const name of ["Add device", "Change profile"]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
    }
    for (const name of ["Remove", "Replace"]) {
      for (const button of await screen.findAllByRole("button", { name })) {
        expect(button).toBeDisabled();
      }
    }
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(route("btrfs_balance_cancel")).toHaveBeenCalledWith({
      uuid: "uuid-1",
    });
  });

  it("adds a free device", async () => {
    const user = userEvent.setup();
    render(<BtrfsDevicesCard filesystem={filesystem(1)} />);

    await user.click(screen.getByRole("button", { name: "Add device" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("combobox", { name: "Device" }));
    // Mounted, partitioned and formatted devices are not offered.
    expect(await screen.findAllByRole("option")).toHaveLength(2);
    expect(
      screen.queryByRole("option", { name: /sdy|sdz|sdw/ }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("option", { name: "/dev/sdx · 50 GB · Spare" }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Add" }));

    expect(route("btrfs_device_add")).toHaveBeenCalledWith(
      { uuid: "uuid-1", device: "/dev/sdx", force: false },
      expect.anything(),
    );
  });

  it("offers formatted devices once force is on", async () => {
    const user = userEvent.setup();
    render(<BtrfsDevicesCard filesystem={filesystem(1)} />);

    await user.click(screen.getByRole("button", { name: "Add device" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "Force (wipe existing filesystem signature)",
      }),
    );
    await user.click(within(dialog).getByRole("combobox", { name: "Device" }));

    expect(
      await screen.findByRole("option", { name: /\/dev\/sdy/ }),
    ).toBeInTheDocument();
  });

  it("confirms before removing a device", async () => {
    const user = userEvent.setup();
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    const row = (await screen.findAllByRole("row"))[2];
    await user.click(within(row).getByRole("button", { name: "Remove" }));
    expect(mocks.submit).not.toHaveBeenCalled();
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(
        "Remove /dev/sdb from the filesystem? Data is migrated to the remaining devices first, which can take a long time.",
      ),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    expect(mocks.submit).toHaveBeenCalledWith("btrfs_device_remove", {
      uuid: "uuid-1",
      deviceId: 2,
    });
  });

  it("replaces a device with a free one", async () => {
    const user = userEvent.setup();
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    const row = (await screen.findAllByRole("row"))[1];
    await user.click(within(row).getByRole("button", { name: "Replace" }));
    const dialog = await screen.findByRole("dialog");
    await pick(user, dialog, "New device", "/dev/sdx · 50 GB · Spare");
    await user.click(within(dialog).getByRole("button", { name: "Replace" }));

    expect(mocks.submit).toHaveBeenCalledWith("btrfs_device_replace", {
      uuid: "uuid-1",
      deviceId: 1,
      newDevice: "/dev/sdx",
    });
  });

  it("starts a scrub from the health card, or stops a running one", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<BtrfsHealthTile filesystem={filesystem(1)} />);
    await user.click(screen.getByRole("button", { name: "Check for errors" }));
    expect(mocks.submit).toHaveBeenCalledWith("btrfs_scrub_start", {
      uuid: "uuid-1",
    });
    unmount();

    const running = filesystem(1);
    running.scrub.status = "running";
    render(<BtrfsHealthTile filesystem={running} />);
    expect(
      screen.queryByRole("button", { name: "Check for errors" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Stop check" }));
    expect(route("btrfs_scrub_cancel")).toHaveBeenCalledWith({
      uuid: "uuid-1",
    });
  });
});
