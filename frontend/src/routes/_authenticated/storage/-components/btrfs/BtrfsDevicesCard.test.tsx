import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  BtrfsFilesystem,
  StorageBlockDevice,
  StorageTopologyInventory,
} from "@/api";
import { render } from "@/test/render";

import BtrfsDevicesCard from "./BtrfsDevicesCard";
import BtrfsHealthBanner from "./BtrfsHealthBanner";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
  pending: new Set<string>(),
  submit: vi.fn(),
  tasks: vi.fn(),
  cancelTask: vi.fn(),
  topology: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { createMutationJobMock } = await import("@/test/mutationJob");
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
  });
  const jobs = new Set([
    "btrfs_balance",
    "btrfs_device_remove",
    "btrfs_device_replace",
    "btrfs_scrub_start",
  ]);
  // The long operations are jobs; the rest answer from the call mock.
  const useMutation = (endpoint: { route: string }, config: never) => {
    const call = useCallMutation(endpoint);
    const [, name] = endpoint.route.split(".");
    if (!jobs.has(name)) return call;
    return {
      isPending: mocks.pending.has(name),
      ...createMutationJobMock(
        {
          follow: () => Promise.resolve(undefined),
          start: (request) => mocks.submit(name, request),
        },
        config,
      ),
    };
  };
  const mocked = {
    ...actual,
    call: (route: string, args: unknown) =>
      route === "tasks.list"
        ? Promise.resolve(mocks.tasks())
        : route === "tasks.cancel"
          ? mocks.cancelTask(args)
          : Promise.reject(),
    useCallMutation,
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        get_topology: {
          queryKey: ["test-get-topology"],
          queryFn: () => mocks.topology(),
        },
      },
    },
  };
  return {
    ...mocked,
    linuxio: mockMutationHooks(mocked.linuxio, useMutation),
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
    physicalBytes: 0,
    growable: false,
    errors: noErrors,
  })),
  scrub: {
    status: "none",
    errorCount: 0,
    uncorrectableErrors: 0,
    correctedErrors: 0,
    bytesScrubbed: 0,
    percent: 0,
  },
  degraded: false,
  lowMetadataSpace: false,
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
  freeBytes: 0,
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
  mocks.tasks.mockReturnValue([]);
  mocks.cancelTask.mockResolvedValue(undefined);
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
    await pick(user, dialog, "Metadata profile", "raid1");
    await user.click(within(dialog).getByRole("button", { name: "Balance" }));

    expect(mocks.submit.mock.calls[0][1]).toEqual({
      uuid: "uuid-1",
      metadataProfile: "raid1",
    });
    expect(mocks.submit.mock.calls[0][1]).not.toHaveProperty("dataProfile");
  });

  it("converts metadata weaker than the new data profile with it", async () => {
    const user = userEvent.setup();
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    const dialog = await openBalance(user);
    await pick(user, dialog, "Data profile", "raid1");
    const metadata = within(dialog).getByRole("combobox", {
      name: "Metadata profile",
    });
    // The current dup metadata would lose the pool with one disk.
    expect(metadata).toHaveTextContent("raid1");
    await user.click(metadata);
    expect(
      screen.queryByRole("option", { name: "Keep current (dup)" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "dup" }),
    ).not.toBeInTheDocument();
    await user.click(metadata);
    await user.click(within(dialog).getByRole("button", { name: "Balance" }));

    expect(mocks.submit).toHaveBeenCalledWith("btrfs_balance", {
      uuid: "uuid-1",
      dataProfile: "raid1",
      metadataProfile: "raid1",
    });
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
  });

  const balanceJob = (id: string, uuid: string, state = "running") => ({
    id,
    metadata: { identity: [uuid] },
    state,
    type: "storage.btrfs_balance",
  });

  it("cancels the running balance through its job", async () => {
    const user = userEvent.setup();
    mocks.pending.add("btrfs_balance");
    mocks.tasks.mockReturnValue([
      balanceJob("other-fs", "uuid-2"),
      balanceJob("queued", "uuid-1", "queued"),
      balanceJob("mine", "uuid-1"),
    ]);
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    await user.click(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(mocks.cancelTask).toHaveBeenCalledWith({ taskId: "mine" }),
    );
    expect(mocks.cancelTask).toHaveBeenCalledTimes(1);
    expect(
      mocks.mutations["storage.btrfs_balance_cancel"],
    ).not.toHaveBeenCalled();
    // The balance stops once the kernel finishes the chunk in hand.
    expect(screen.getByRole("button", { name: "Cancelling…" })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Cancel" }),
    ).not.toBeInTheDocument();
  });

  it("offers the cancel again when the request fails", async () => {
    const user = userEvent.setup();
    mocks.pending.add("btrfs_balance");
    mocks.tasks.mockReturnValue([balanceJob("mine", "uuid-1")]);
    mocks.cancelTask.mockRejectedValueOnce(new Error("bridge offline"));
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    await user.click(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(mocks.cancelTask).toHaveBeenCalledTimes(1));
    expect(await screen.findByRole("button", { name: "Cancel" })).toBeEnabled();
  });

  it("cancels in the kernel when the balance has no running job", async () => {
    const user = userEvent.setup();
    mocks.pending.add("btrfs_balance");
    mocks.tasks.mockReturnValue([balanceJob("other-fs", "uuid-2")]);
    render(<BtrfsDevicesCard filesystem={filesystem(2)} />);

    await user.click(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(route("btrfs_balance_cancel")).toHaveBeenCalledWith({
        uuid: "uuid-1",
      }),
    );
    expect(mocks.cancelTask).not.toHaveBeenCalled();
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
        name: /^Force: erase an old filesystem/,
      }),
    );
    await user.click(within(dialog).getByRole("combobox", { name: "Device" }));

    expect(
      await screen.findByRole("option", { name: /\/dev\/sdy/ }),
    ).toBeInTheDocument();
  });

  it("keeps a disk of another pool, array or volume group disabled with its reason, force or not", async () => {
    const user = userEvent.setup();
    mocks.topology.mockResolvedValue(
      topology([
        blockDevice({}),
        blockDevice({
          path: "/dev/sdh",
          name: "sdh",
          fsType: "btrfs",
          member: { kind: "btrfs", name: "old", uuid: "o1" },
        }),
        blockDevice({
          path: "/dev/sdl",
          name: "sdl",
          fsType: "linux_raid_member",
          member: { kind: "md", name: "host:data", uuid: "a1" },
        }),
        blockDevice({
          path: "/dev/sdm",
          name: "sdm",
          fsType: "LVM2_member",
          member: { kind: "lvm", name: "vg0", uuid: "pv1" },
        }),
        blockDevice({ path: "/dev/sdv", name: "sdv", busy: "Active swap" }),
      ]),
    );
    render(<BtrfsDevicesCard filesystem={filesystem(1)} />);

    await user.click(screen.getByRole("button", { name: "Add device" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("checkbox", { name: /^Force: erase/ }),
    );
    await user.click(within(dialog).getByRole("combobox", { name: "Device" }));
    for (const reason of [
      "/dev/sdh · 50 GB · Spare · Member of btrfs pool old",
      "/dev/sdl · 50 GB · Spare · Member of md array host:data",
      "/dev/sdm · 50 GB · Spare · Member of LVM volume group vg0",
      "/dev/sdv · 50 GB · Spare · Active swap",
    ]) {
      expect(
        await screen.findByRole("option", { name: reason }),
      ).toHaveAttribute("aria-disabled", "true");
    }
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

  it("starts a scrub from the status strip, or stops a running one", async () => {
    const user = userEvent.setup();
    const { unmount } = render(
      <BtrfsHealthBanner filesystem={filesystem(1)} />,
    );
    await user.click(screen.getByRole("button", { name: "Check for errors" }));
    expect(mocks.submit).toHaveBeenCalledWith("btrfs_scrub_start", {
      uuid: "uuid-1",
    });
    unmount();

    const running = filesystem(1);
    running.scrub.status = "running";
    render(<BtrfsHealthBanner filesystem={running} />);
    expect(
      screen.queryByRole("button", { name: "Check for errors" }),
    ).not.toBeInTheDocument();
    // No scrub job (the timer started it): the kernel cancel stops it.
    await user.click(screen.getByRole("button", { name: "Stop check" }));
    await waitFor(() =>
      expect(route("btrfs_scrub_cancel")).toHaveBeenCalledWith({
        uuid: "uuid-1",
      }),
    );
    expect(mocks.cancelTask).not.toHaveBeenCalled();
  });

  it("stops a running scrub through its job", async () => {
    const user = userEvent.setup();
    const scrubJob = (id: string, uuid: string, state = "running") => ({
      id,
      metadata: { identity: [uuid, "scrub"] },
      state,
      type: "storage.btrfs_scrub_start",
    });
    mocks.tasks.mockReturnValue([
      scrubJob("other-fs", "uuid-2"),
      scrubJob("queued", "uuid-1", "queued"),
      scrubJob("mine", "uuid-1"),
    ]);
    const running = filesystem(1);
    running.scrub.status = "running";
    render(<BtrfsHealthBanner filesystem={running} />);

    await user.click(screen.getByRole("button", { name: "Stop check" }));

    await waitFor(() =>
      expect(mocks.cancelTask).toHaveBeenCalledWith({ taskId: "mine" }),
    );
    expect(mocks.cancelTask).toHaveBeenCalledTimes(1);
    expect(route("btrfs_scrub_cancel")).not.toHaveBeenCalled();
    // The scrub stops when its job's handler returns.
    expect(screen.getByRole("button", { name: "Stopping…" })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "Stop check" }),
    ).not.toBeInTheDocument();
  });

  it("offers Stop check again when the request fails, and for the next scrub", async () => {
    const user = userEvent.setup();
    mocks.tasks.mockReturnValue([
      {
        id: "mine",
        metadata: { identity: ["uuid-1", "scrub"] },
        state: "running",
        type: "storage.btrfs_scrub_start",
      },
    ]);
    mocks.cancelTask.mockRejectedValueOnce(new Error("bridge offline"));
    const running = filesystem(1);
    running.scrub = {
      ...running.scrub,
      startedAt: "2026-11-09T10:00:00Z",
      status: "running",
    };
    const view = render(<BtrfsHealthBanner filesystem={running} />);

    await user.click(screen.getByRole("button", { name: "Stop check" }));
    await waitFor(() => expect(mocks.cancelTask).toHaveBeenCalledTimes(1));
    expect(
      await screen.findByRole("button", { name: "Stop check" }),
    ).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Stop check" }));
    expect(
      await screen.findByRole("button", { name: "Stopping…" }),
    ).toBeDisabled();
    // A later scrub is a new one to stop.
    const next = filesystem(1);
    next.scrub = {
      ...next.scrub,
      startedAt: "2026-11-09T12:00:00Z",
      status: "running",
    };
    view.rerender(<BtrfsHealthBanner filesystem={next} />);
    expect(screen.getByRole("button", { name: "Stop check" })).toBeEnabled();
  });

  it("offers to grow a device whose disk is bigger than the filesystem's view", async () => {
    const user = userEvent.setup();
    const fs = filesystem(2);
    fs.devices[1] = {
      ...fs.devices[1],
      physicalBytes: 200 * GiB,
      growable: true,
    };
    render(<BtrfsDevicesCard filesystem={fs} />);

    expect(
      screen.getAllByRole("button", { name: "Grow to disk size" }),
    ).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Grow to disk size" }));
    expect(mocks.mutations["storage.btrfs_device_grow"]).toHaveBeenCalledWith({
      uuid: "uuid-1",
      deviceId: 2,
    });
  });
});
