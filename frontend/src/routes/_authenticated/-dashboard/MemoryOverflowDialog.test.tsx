import { fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { SwapStatus } from "@/api";
import { linuxio, LinuxIOError } from "@/api";
import {
  foreignZramConfig,
  generatorDevice,
  swapFileCreate,
  swapFileDevice,
  swapFileRow,
  swapModeResult,
  swapPartitionDevice,
  swapStatus,
  zramDevice,
} from "@/test/memoryStatusFixture";
import {
  act,
  createTestQueryClient,
  renderWithTanStackRouter,
  screen,
  waitFor,
} from "@/test/render";

import MemoryOverflowDialog from "./MemoryOverflowDialog";

const GIB = 1024 ** 3;

const mocks = vi.hoisted(() => ({
  // What the next mutation resolves with, by route, and the status the
  // host reports meanwhile (a mutation moves it, as the real host does).
  callFails: false,
  failWith: null as Error | null,
  hold: null as Promise<unknown> | null,
  onClose: vi.fn(),
  progress: null as string | null,
  progressDetail: null as unknown,
  requests: [] as { request: unknown; route: string }[],
  results: {} as Record<string, unknown>,
  status: null as unknown,
  statusHold: null as Promise<unknown> | null,
  // The jobs the host reports running, and the ones the dialog took up.
  tasks: [] as unknown[],
  watched: [] as string[],
  watchResult: null as unknown,
  toast: {
    error: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
}));

vi.mock("sonner", () => ({ toast: mocks.toast }));
vi.mock("@/api/calls", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/calls")>()),
  call: vi.fn(async (route: string) => {
    if (route === "tasks.list") return mocks.tasks;
    if (mocks.statusHold) await mocks.statusHold;
    if (mocks.callFails) throw new Error("the bridge did not answer");
    return mocks.status;
  }),
}));
vi.mock("@/api/linuxio-core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/linuxio-core")>()),
  mutationRequest: async (
    handler: string,
    command: string,
    request: unknown,
    handlers: {
      onProgress?: (progress: unknown) => void;
      onSnapshot?: (snapshot: unknown) => void;
    },
  ) => {
    const route = `${handler}.${command}`;
    mocks.requests.push({ request, route });
    handlers.onSnapshot?.({ id: "task-1" });
    if (mocks.progress) {
      handlers.onProgress?.({
        detail: mocks.progressDetail,
        message: mocks.progress,
      });
    }
    if (mocks.hold) await mocks.hold;
    if (mocks.failWith) throw mocks.failWith;
    const result = mocks.results[route];
    mocks.status = result;
    return result;
  },
  watchMutation: async (
    taskId: string,
    handlers: {
      onProgress?: (progress: unknown) => void;
      onSnapshot?: (snapshot: unknown) => void;
    },
  ) => {
    mocks.watched.push(taskId);
    handlers.onSnapshot?.({ id: taskId });
    if (mocks.progress) {
      handlers.onProgress?.({
        detail: mocks.progressDetail,
        message: mocks.progress,
      });
    }
    if (mocks.hold) await mocks.hold;
    if (mocks.failWith) throw mocks.failWith;
    mocks.status = mocks.watchResult;
    return mocks.watchResult;
  },
}));

const diskStatus = (overrides: Partial<SwapStatus> = {}) =>
  swapStatus({
    devices: [swapFileDevice()],
    mode: "disk",
    recommended_mode: "zswap",
    swap_files: [swapFileRow()],
    swappiness: 60,
    ...overrides,
  });

// The dialog fades in over a few frames, and what is in it counts as visible
// only once that has started.
const dialogShown = () =>
  waitFor(() => expect(screen.getByRole("dialog")).toBeVisible());

const renderSection = async (status: SwapStatus) => {
  mocks.status = status;
  const queryClient = createTestQueryClient();
  const view = renderWithTanStackRouter(
    <MemoryOverflowDialog onClose={mocks.onClose} open />,
    {
      auth: { privileged: true },
      queryClient,
    },
  );
  await screen.findByRole("radiogroup", { name: "When memory runs out" });
  await dialogShown();
  return { ...view, queryClient };
};

const radio = (name: string) => screen.getByRole("radio", { name });

// The banner at the top; the running step's caption is a status too.
const banner = () => screen.getAllByRole("status")[0];

// The dialog while it asks before switching to compressed memory.
const confirmStep = () =>
  screen.queryByRole("dialog", { name: "Switch to compressed memory?" });

// Lets the wait after a key press on the eagerness slider run out.
const passKeyWait = () => act(() => vi.advanceTimersByTimeAsync(500));

afterEach(() => {
  vi.useRealTimers();
});

beforeEach(() => {
  mocks.callFails = false;
  mocks.failWith = null;
  mocks.hold = null;
  mocks.onClose.mockClear();
  mocks.progress = null;
  mocks.progressDetail = null;
  mocks.requests = [];
  mocks.results = {};
  mocks.tasks = [];
  mocks.watched = [];
  mocks.watchResult = null;
  mocks.statusHold = null;
  mocks.toast.error.mockClear();
  mocks.toast.success.mockClear();
  mocks.toast.warning.mockClear();
});

describe("MemoryOverflowDialog banner", () => {
  it("offers compressed memory when nothing absorbs overflow", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(swapStatus());
    const { user } = await renderSection(
      swapStatus({ devices: [], mode: "none", swappiness: 60 }),
    );

    expect(
      screen.getByText("If memory runs out, Linux will close programs."),
    ).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Turn on compressed memory" }),
    );

    await waitFor(() =>
      expect(mocks.requests).toEqual([
        { request: { mode: "zram" }, route: "memory.set_swap_mode" },
      ]),
    );
  });

  it("points to Capabilities when no zram tool is installed", async () => {
    await renderSection(
      swapStatus({
        devices: [],
        mode: "none",
        zram_available: false,
        zram_provider: "",
        zram_unavailable_reason: "package_missing",
      }),
    );

    expect(
      screen.queryByRole("button", { name: "Turn on compressed memory" }),
    ).not.toBeInTheDocument();
    const links = screen.getAllByRole("link", {
      name: "Open Capabilities to install zram",
    });
    // One in the banner, one where the zram radio would be.
    expect(links).toHaveLength(2);
    expect(links[0]).toHaveAttribute("href", "/settings?tab=capabilities");
    expect(
      screen.queryByRole("radio", { name: "Compressed memory only" }),
    ).not.toBeInTheDocument();
  });

  it("offers compressed memory beside a swap partition, which stays on", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(swapStatus());
    const { user } = await renderSection(
      swapStatus({
        devices: [swapPartitionDevice()],
        mode: "disk",
        swappiness: 60,
      }),
    );

    expect(radio("Compressed memory only")).toBeEnabled();
    expect(radio("Compressed memory only")).toHaveAccessibleDescription(
      /\/dev\/vdb1 stays on and takes what compressed memory cannot hold\./,
    );
    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    // Nothing is removed, so nothing is asked.
    await waitFor(() =>
      expect(mocks.requests).toEqual([
        { request: { mode: "zram" }, route: "memory.set_swap_mode" },
      ]),
    );
    expect(confirmStep()).not.toBeInTheDocument();
  });

  it("asks to finish a switch that left both active", async () => {
    await renderSection(
      swapStatus({
        devices: [zramDevice, swapFileDevice(GIB)],
        mode: "mixed",
        swap_files: [swapFileRow({ used: GIB })],
      }),
    );

    expect(
      screen.getByText(
        "Compressed memory and a swap file are both active. Choose one below and apply to finish.",
      ),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Bring back to memory" }),
    ).not.toBeInTheDocument();
  });

  it("brings parked data back", async () => {
    mocks.results["memory.drain_swap"] = diskStatus();
    const { user } = await renderSection(
      diskStatus({
        can_drain: true,
        devices: [swapFileDevice(1.5 * GIB)],
        drain_blocked_reason: "",
      }),
    );

    expect(
      screen.getByText(
        "1.5 GB of memory is parked on the system drive. Programs that need it are slower until it comes back.",
      ),
    ).toBeVisible();
    await user.click(
      screen.getByRole("button", { name: "Bring back to memory" }),
    );

    await waitFor(() =>
      expect(mocks.requests).toEqual([
        { request: {}, route: "memory.drain_swap" },
      ]),
    );
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Parked data brought back to memory",
        undefined,
      ),
    );
  });

  it.each([
    ["not_enough_memory", "Not enough free memory right now"],
    [
      "other_swap",
      "The data is on /dev/vdb1, which LinuxIO does not turn off, so it stays there",
    ],
  ] as const)("disables the drain and says why for %s", async (code, hint) => {
    await renderSection(
      diskStatus({
        can_drain: false,
        devices:
          code === "other_swap"
            ? [swapFileDevice(), swapPartitionDevice(GIB)]
            : [swapFileDevice(GIB)],
        drain_blocked_reason: code,
      }),
    );

    expect(
      screen.getByText(/1 GB of memory is parked on the system drive/),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Bring back to memory" }),
    ).toBeDisabled();
    expect(screen.getByText(hint)).toBeVisible();
  });

  it("disables the drain without a hint for a reason it does not know", async () => {
    await renderSection(
      diskStatus({
        can_drain: false,
        devices: [swapFileDevice(GIB)],
        drain_blocked_reason: "something_new" as never,
      }),
    );

    expect(
      screen.getByRole("button", { name: "Bring back to memory" }),
    ).toBeDisabled();
  });

  it("warns that a btrfs swap file blocks snapshots and offers the switch", async () => {
    const { user } = await renderSection(
      diskStatus({
        devices: [swapFileDevice(0, "btrfs")],
        swap_files: [swapFileRow({ blocks_snapshots: true, fs_type: "btrfs" })],
      }),
    );

    expect(banner()).toHaveTextContent(
      "/swap.img blocks snapshots of the subvolume it is in.",
    );
    await user.click(
      screen.getByRole("button", { name: "Switch to compressed memory" }),
    );

    expect(
      await screen.findByRole("dialog", {
        name: "Switch to compressed memory?",
      }),
    ).toBeVisible();
    expect(mocks.requests).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(radio("Compressed memory only")).toBeChecked();
  });

  it("hides the switch when zram cannot be chosen", async () => {
    await renderSection(
      diskStatus({
        devices: [swapFileDevice(0, "btrfs")],
        swap_files: [swapFileRow({ blocks_snapshots: true, fs_type: "btrfs" })],
        zram_available: false,
        zram_provider: "",
        zram_unavailable_reason: "package_missing",
      }),
    );

    expect(banner()).toHaveTextContent(
      "/swap.img blocks snapshots of the subvolume it is in.",
    );
    expect(
      screen.queryByRole("button", { name: "Switch to compressed memory" }),
    ).not.toBeInTheDocument();
  });

  it("offers no switch for a btrfs swap file fstab does not name", async () => {
    await renderSection(
      diskStatus({
        devices: [swapFileDevice(0, "btrfs")],
        swap_files: [
          swapFileRow({
            blocks_snapshots: true,
            fs_type: "btrfs",
            locked_reason: "not_in_fstab",
          }),
        ],
      }),
    );

    expect(banner()).toHaveTextContent(
      "/swap.img blocks snapshots of the subvolume it is in.",
    );
    expect(
      screen.queryByRole("button", { name: "Switch to compressed memory" }),
    ).not.toBeInTheDocument();
  });

  it("says memory is fine when nothing is parked", async () => {
    await renderSection(swapStatus());

    expect(
      screen.getByText("Memory is fine. Nothing is parked."),
    ).toBeVisible();
  });
});

describe("MemoryOverflowDialog rows", () => {
  it("lists the three rows in order with their chips", async () => {
    await renderSection(diskStatus());

    expect(
      screen.getAllByRole("radio").map((input) => input.getAttribute("value")),
    ).toEqual(["zswap", "zram", "disk"]);
    expect(
      screen.getByRole("radio", {
        name: "Compressed memory with the disk as backup",
      }),
    ).toBeEnabled();
    expect(screen.getAllByText("Recommended")).toHaveLength(1);
    expect(screen.getAllByText("Current")).toHaveLength(1);
    expect(radio("Disk only")).toBeChecked();
    // The technical name appears once, as a caption.
    expect(screen.getAllByText("zswap")).toHaveLength(1);
    expect(screen.getByText("swap file")).toBeVisible();
    expect(
      screen.getByText(
        "No backup. If it fills up, Linux closes the hungriest program, and a VM would stop like a power cut.",
      ),
    ).toBeVisible();
    // An ext4 swap file blocks no snapshots, so neither row speaks of them.
    expect(
      screen.queryByText("Blocks snapshots of the system drive while in use."),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/Writes nothing to disk\. Holds about/),
    ).toBeVisible();
    expect(radio("Disk only")).toHaveAccessibleDescription(
      /Rarely used data goes straight to the swap file/,
    );
    // Chips are part of what a screen reader hears for the row.
    expect(radio("Disk only")).toHaveAccessibleDescription(/^Current/);
    expect(
      radio("Compressed memory with the disk as backup"),
    ).toHaveAccessibleDescription(/^Recommended/);
  });

  it("marks the recommended row when the host matches none", async () => {
    await renderSection(
      swapStatus({ devices: [], mode: "none", recommended_mode: "zram" }),
    );

    expect(screen.getAllByText("Recommended")).toHaveLength(1);
    expect(screen.queryByText("Current")).not.toBeInTheDocument();
    expect(radio("Compressed memory only")).toBeChecked();
  });

  it("disables the rows that need a swap file LinuxIO cannot create, with their reason", async () => {
    await renderSection(
      swapStatus({
        create: swapFileCreate({ blocked_reason: "not_enough_space" }),
      }),
    );

    const reason =
      "Needs a swap file, and LinuxIO cannot create one: the disk has 40 GB free, and a swap file of at least 256 MB must still leave 2 GB free.";
    expect(radio("Compressed memory with the disk as backup")).toBeDisabled();
    expect(radio("Disk only")).toBeDisabled();
    expect(screen.getAllByText(reason)).toHaveLength(2);
    expect(radio("Disk only")).toHaveAccessibleDescription(
      expect.stringContaining(reason),
    );
  });

  it("disables the rows that need disk swap while a swap file in fstab is off", async () => {
    await renderSection(
      swapStatus({
        swap_files: [
          swapFileRow({ active: false, resize_blocked_reason: "off" }),
        ],
      }),
    );

    expect(radio("Disk only")).toBeDisabled();
    expect(
      screen.getAllByText(
        "/swap.img is in /etc/fstab and off. LinuxIO does not turn it on: remove it under Swap files to choose this.",
      ),
    ).toHaveLength(2);
  });

  it("disables zswap on a kernel without it", async () => {
    await renderSection(
      diskStatus({
        zswap: {
          available: false,
          compressor: "",
          enabled: false,
          max_pool_percent: 0,
        },
      }),
    );

    expect(radio("Compressed memory with the disk as backup")).toBeDisabled();
    expect(screen.getByText("This kernel has no zswap")).toBeVisible();
  });

  it("says what zram will hold and cost", async () => {
    await renderSection(swapStatus({ zram_planned_size: 6 * GIB }));

    expect(
      screen.getByText(/Holds about 6 GB using about 2 GB of memory\./),
    ).toBeVisible();
  });
});

describe("MemoryOverflowDialog refresh", () => {
  it("keeps its content when a background refresh fails", async () => {
    const { queryClient } = await renderSection(diskStatus());

    mocks.callFails = true;
    await act(async () => {
      await queryClient.invalidateQueries({
        queryKey: linuxio.memory.get_swap_status.queryKey,
      });
    });

    await waitFor(() =>
      expect(
        queryClient.getQueryState(linuxio.memory.get_swap_status.queryKey)
          ?.status,
      ).toBe("error"),
    );
    expect(
      screen.queryByText("Memory status unavailable"),
    ).not.toBeInTheDocument();
    expect(radio("Disk only")).toBeChecked();
  });

  it("keeps focus on Close when the status arrives", async () => {
    let answer: () => void = () => {};
    const waiting = new Promise<void>((resolve) => {
      answer = resolve;
    });
    mocks.statusHold = waiting;
    mocks.status = diskStatus();
    renderWithTanStackRouter(
      <MemoryOverflowDialog onClose={mocks.onClose} open />,
      { auth: { privileged: true } },
    );
    const close = await screen.findByRole("button", { name: "Close" });
    close.focus();

    answer();
    await screen.findByRole("radiogroup", { name: "When memory runs out" });

    expect(screen.getByRole("button", { name: "Close" })).toHaveFocus();
  });

  it("shows the error when there has never been a status", async () => {
    mocks.callFails = true;
    renderWithTanStackRouter(
      <MemoryOverflowDialog onClose={mocks.onClose} open />,
      {
        auth: { privileged: true },
      },
    );

    await screen.findByText("Memory status unavailable");
    await dialogShown();
    expect(screen.getByText("Memory status unavailable")).toBeVisible();
    expect(screen.getByText("the bridge did not answer")).toBeVisible();
  });
});

describe("MemoryOverflowDialog row text", () => {
  it("selects a row from its description and its technical caption", async () => {
    const { user } = await renderSection(diskStatus());

    await user.click(
      screen.getByText(/^Squeezes rarely used data to about a third/),
    );
    expect(radio("Compressed memory with the disk as backup")).toBeChecked();

    await user.click(screen.getByText("zram (zram-tools)"));
    expect(radio("Compressed memory only")).toBeChecked();
  });

  it("keeps the radio's name to the row name", async () => {
    await renderSection(diskStatus());

    expect(
      screen.getByRole("radio", { name: "Compressed memory only" }),
    ).toBeVisible();
  });

  it("leaves the Capabilities link outside any label", async () => {
    await renderSection(
      swapStatus({
        zram_available: false,
        zram_provider: "",
        zram_unavailable_reason: "package_missing",
      }),
    );

    for (const link of screen.getAllByRole("link")) {
      expect(link.closest("label")).toBeNull();
    }
  });
});

describe("MemoryOverflowDialog apply", () => {
  it("stays off until another row is chosen", async () => {
    const { user } = await renderSection(diskStatus());

    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    await user.click(radio("Compressed memory with the disk as backup"));
    expect(screen.getByRole("button", { name: "Apply" })).toBeEnabled();
  });

  it("is on for the mode already chosen while both are active", async () => {
    await renderSection(
      swapStatus({
        devices: [zramDevice, swapFileDevice(GIB)],
        mode: "mixed",
        swap_files: [swapFileRow({ used: GIB })],
      }),
    );

    expect(radio("Compressed memory only")).toBeChecked();
    expect(screen.getByRole("button", { name: "Apply" })).toBeEnabled();
  });

  it("sends the selected mode without a dialog for zswap", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(
      diskStatus({ mode: "zswap", swappiness: 60 }),
    );
    const { user } = await renderSection(diskStatus());

    await user.click(radio("Compressed memory with the disk as backup"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(mocks.requests).toEqual([
        { request: { mode: "zswap" }, route: "memory.set_swap_mode" },
      ]),
    );
    expect(confirmStep()).not.toBeInTheDocument();
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Memory overflow setting applied",
        undefined,
      ),
    );
    expect(mocks.toast.warning).not.toHaveBeenCalled();
  });

  it("confirms before turning zram on, naming each swap file it removes", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(swapStatus());
    const { user } = await renderSection(
      diskStatus({
        devices: [swapFileDevice(), swapFileDevice(0, "ext4", "/swap2.img")],
        swap_files: [
          swapFileRow(),
          swapFileRow({ path: "/swap2.img", size: 20 * GIB }),
        ],
      }),
    );

    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    const dialog = await screen.findByRole("dialog", {
      name: "Switch to compressed memory?",
    });
    expect(dialog).toHaveTextContent("Compressed memory is turned on.");
    expect(dialog).toHaveTextContent("/swap.img, 4 GB, is removed.");
    expect(dialog).toHaveTextContent("/swap2.img, 20 GB, is removed.");
    expect(dialog).toHaveTextContent(
      "Data parked in a swap file is brought back to memory before the file is turned off and deleted.",
    );
    expect(mocks.requests).toEqual([]);
    // The step opens on the way back, and going back returns to Apply.
    expect(screen.getByRole("button", { name: "Back" })).toHaveFocus();

    await user.click(screen.getByRole("button", { name: "Back" }));
    await waitFor(() => expect(confirmStep()).not.toBeInTheDocument());
    expect(
      screen.getByRole("dialog", { name: "Memory overflow" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Apply" })).toHaveFocus();
    expect(mocks.requests).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Apply" }));
    await user.click(
      await screen.findByRole("button", { name: "Turn on compressed memory" }),
    );
    // The request names the files the step listed.
    await waitFor(() =>
      expect(mocks.requests).toEqual([
        {
          request: {
            confirm_remove: ["/swap.img", "/swap2.img"],
            mode: "zram",
          },
          route: "memory.set_swap_mode",
        },
      ]),
    );
    await waitFor(() => expect(confirmStep()).not.toBeInTheDocument());
  });

  it("does not ask when zram goes on and no swap file is removed", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(swapStatus());
    const { user } = await renderSection(
      diskStatus({ devices: [], mode: "none", swap_files: [] }),
    );

    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(mocks.requests).toHaveLength(1));
    expect(confirmStep()).not.toBeInTheDocument();
  });

  it("does not ask before zram when the active swap file is not in fstab", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(swapStatus());
    const { user } = await renderSection(
      diskStatus({
        swap_files: [swapFileRow({ locked_reason: "not_in_fstab" })],
      }),
    );

    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(mocks.requests).toHaveLength(1));
    expect(confirmStep()).not.toBeInTheDocument();
  });

  it("names a zram configuration file that is not LinuxIO's and sends that it may be replaced", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(swapStatus());
    const { user } = await renderSection(
      diskStatus({ foreign_zram_config: foreignZramConfig() }),
    );

    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    // One step lists the swap file and the configuration file.
    const dialog = await screen.findByRole("dialog", {
      name: "Switch to compressed memory?",
    });
    expect(dialog).toHaveTextContent("/swap.img, 4 GB, is removed.");
    expect(dialog).toHaveTextContent(
      "/etc/default/zramswap holds zram settings that are not LinuxIO's. It is replaced, and a copy is kept as /etc/default/zramswap.before-linuxio.",
    );
    expect(screen.getByRole("button", { name: "Back" })).toHaveFocus();
    expect(mocks.requests).toEqual([]);

    await user.click(
      screen.getByRole("button", { name: "Turn on compressed memory" }),
    );
    await waitFor(() =>
      expect(mocks.requests).toEqual([
        {
          request: {
            confirm_remove: ["/swap.img"],
            confirm_replace: "/etc/default/zramswap",
            mode: "zram",
          },
          route: "memory.set_swap_mode",
        },
      ]),
    );
  });

  it("asks before leaving zram when zram-generator's file is not LinuxIO's", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(
      diskStatus({ zram_provider: "zram-generator" }),
    );
    const { user } = await renderSection(
      swapStatus({
        devices: [zramDevice, swapPartitionDevice()],
        foreign_zram_config: foreignZramConfig(
          "/etc/systemd/zram-generator.conf",
        ),
        zram_provider: "zram-generator",
      }),
    );

    await user.click(radio("Disk only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    const dialog = await screen.findByRole("dialog", {
      name: "Switch to disk only?",
    });
    expect(dialog).toHaveTextContent(
      "/etc/systemd/zram-generator.conf holds zram settings that are not LinuxIO's. It is replaced, and a copy is kept as /etc/systemd/zram-generator.conf.before-linuxio.",
    );
    expect(screen.getByRole("button", { name: "Back" })).toHaveFocus();

    await user.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() =>
      expect(mocks.requests).toEqual([
        {
          request: {
            confirm_replace: "/etc/systemd/zram-generator.conf",
            mode: "disk",
          },
          route: "memory.set_swap_mode",
        },
      ]),
    );
  });

  it("shows the other zram-generator files with the size each sets, and why leaving zram is refused", async () => {
    await renderSection(
      swapStatus({
        zram_generator_devices: [generatorDevice()],
        zram_provider: "zram-generator",
      }),
    );

    expect(
      screen.getByText(
        "zram-generator also reads these files, and what they set wins over the settings LinuxIO writes:",
      ),
    ).toBeVisible();
    expect(
      screen.getByText(
        "/etc/systemd/zram-generator.conf.d/50-big.conf sets up zram1 with zram-size = 2048",
      ),
    ).toBeVisible();
    expect(radio("Disk only")).toBeDisabled();
    expect(radio("Compressed memory with the disk as backup")).toBeDisabled();
    expect(
      screen.getAllByText(
        "/etc/systemd/zram-generator.conf.d/50-big.conf turns compressed memory on at every start. LinuxIO does not change that file. To choose this, remove that file or take the device out of it.",
      ),
    ).toHaveLength(2);
  });

  it("names the swap file a switch to zram left on", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(
      swapStatus({
        devices: [zramDevice, swapFileDevice(GIB)],
        mode: "mixed",
        swap_files: [swapFileRow({ used: GIB })],
      }),
      [{ path: "/swap.img", reason: "zram_not_swapping", what: "swap_file" }],
    );
    const { user } = await renderSection(diskStatus());

    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    await user.click(
      await screen.findByRole("button", { name: "Turn on compressed memory" }),
    );

    await waitFor(() =>
      expect(mocks.toast.warning).toHaveBeenCalledWith(
        "Compressed memory is not running, so /swap.img stays on.",
        undefined,
      ),
    );
    expect(mocks.toast.success).not.toHaveBeenCalled();
  });

  it("warns when the switch could not finish", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(
      swapStatus({
        devices: [zramDevice, swapFileDevice(GIB)],
        mode: "mixed",
        swap_files: [swapFileRow({ used: GIB })],
      }),
      [{ path: "/swap.img", reason: "not_enough_memory", what: "swap_file" }],
    );
    const { user } = await renderSection(
      diskStatus({ devices: [swapFileDevice()] }),
    );

    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    await user.click(
      await screen.findByRole("button", { name: "Turn on compressed memory" }),
    );

    await waitFor(() =>
      expect(mocks.toast.warning).toHaveBeenCalledWith(
        "Not enough free memory to bring back the data in /swap.img, so it stays on. Apply again when more memory is free.",
        undefined,
      ),
    );
    expect(mocks.toast.success).not.toHaveBeenCalled();
    expect(
      await screen.findByText(
        "Compressed memory and a swap file are both active. Choose one below and apply to finish.",
      ),
    ).toBeVisible();
  });

  it("warns that compressed memory kept its size though the mode was reached", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(swapStatus(), [
      { reason: "no_other_swap", what: "zram_size" },
    ]);
    const { user } = await renderSection(swapStatus({ mode: "mixed" }));

    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(mocks.toast.warning).toHaveBeenCalledWith(
        "Compressed memory keeps its current size until the next restart: restarting it now would leave the host without swap.",
        undefined,
      ),
    );
    expect(mocks.toast.success).not.toHaveBeenCalled();
  });

  it("shows the server's reason when the change fails", async () => {
    mocks.failWith = new Error("a swap change is already running");
    const { user } = await renderSection(diskStatus());

    await user.click(radio("Compressed memory with the disk as backup"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(mocks.toast.error).toHaveBeenCalledWith(
        "a swap change is already running",
        undefined,
      ),
    );
  });

  it("does not report a change cancelled from the task tray as a failure", async () => {
    mocks.failWith = new LinuxIOError("operation aborted", 499);
    const { user } = await renderSection(diskStatus());

    await user.click(radio("Compressed memory with the disk as backup"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Apply" })).toBeEnabled(),
    );
    expect(mocks.requests).toHaveLength(1);
    expect(mocks.toast.error).not.toHaveBeenCalled();
    expect(mocks.toast.success).not.toHaveBeenCalled();
  });

  it("shows the running step and holds the controls, but not Close, while a mode change runs", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.progress = "Turning zswap on";
    mocks.results["memory.set_swap_mode"] = swapModeResult(
      diskStatus({ mode: "zswap" }),
    );
    const { user } = await renderSection(
      diskStatus({
        can_drain: true,
        devices: [swapFileDevice(GIB)],
        drain_blocked_reason: "",
      }),
    );
    await user.click(screen.getByRole("button", { name: "Advanced" }));
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    await user.click(radio("Compressed memory with the disk as backup"));
    await user.click(screen.getByRole("button", { name: "Apply" }));

    expect(await screen.findByText("Turning zswap on")).toBeVisible();
    expect(screen.getByRole("button", { name: "Applying…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Bring back to memory" }),
    ).toBeDisabled();
    expect(radio("Disk only")).toBeDisabled();
    expect(slider).toBeDisabled();

    finish();
    await waitFor(() =>
      expect(screen.queryByText("Turning zswap on")).not.toBeInTheDocument(),
    );
  });

  it("closes while a change runs, keeps the change going and reports it", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.progress = "Turning zswap on";
    mocks.results["memory.set_swap_mode"] = swapModeResult(
      diskStatus({ mode: "zswap" }),
    );
    const { user } = await renderSection(diskStatus());

    await user.click(radio("Compressed memory with the disk as backup"));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    expect(await screen.findByText("Turning zswap on")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(mocks.onClose).toHaveBeenCalledTimes(1);
    // The change being applied is still the one the dialog shows.
    expect(screen.getByText("Turning zswap on")).toBeVisible();
    expect(radio("Compressed memory with the disk as backup")).toBeChecked();

    finish();
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Memory overflow setting applied",
        undefined,
      ),
    );
    expect(mocks.requests).toHaveLength(1);
  });

  it("leaves the confirmation as the change starts and holds the first step", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.progress = "Starting zram";
    mocks.results["memory.set_swap_mode"] = swapModeResult(swapStatus());
    const { user } = await renderSection(
      diskStatus({ devices: [swapFileDevice()] }),
    );

    await user.click(radio("Compressed memory only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    await user.click(
      await screen.findByRole("button", { name: "Turn on compressed memory" }),
    );

    await waitFor(() => expect(confirmStep()).not.toBeInTheDocument());
    expect(radio("Compressed memory only")).toBeChecked();
    expect(await screen.findByText("Starting zram")).toBeVisible();
    expect(screen.getByRole("button", { name: "Applying…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Close" })).toHaveFocus();

    await user.keyboard("{Escape}");
    expect(mocks.onClose).toHaveBeenCalledTimes(1);

    finish();
    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledTimes(1));
  });

  it("does not take up a change that the dialog it was started in still follows", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.results["memory.set_swap_mode"] = swapModeResult(
      diskStatus({ mode: "zswap" }),
    );
    const { queryClient, unmount, user } = await renderSection(diskStatus());
    await user.click(radio("Compressed memory with the disk as backup"));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(mocks.requests).toHaveLength(1));

    // The dashboard is left and opened again while the job runs.
    unmount();
    mocks.tasks = [
      {
        id: "task-1",
        metadata: { action: "zswap", identity: ["memory", "swap_mode"] },
        state: "running",
        type: "memory.set_swap_mode",
      },
    ];
    renderWithTanStackRouter(
      <MemoryOverflowDialog onClose={mocks.onClose} open />,
      { auth: { privileged: true }, queryClient },
    );
    await screen.findByRole("radiogroup", { name: "When memory runs out" });
    await dialogShown();

    expect(screen.getByRole("button", { name: "Applying…" })).toBeDisabled();
    expect(mocks.watched).toEqual([]);

    finish();
    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Apply" })).toBeVisible(),
    );
    expect(mocks.toast.success).toHaveBeenCalledTimes(1);
    expect(mocks.watched).toEqual([]);
  });

  it("takes up a mode change that is already running when it opens", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.progress = "Removing the swap file";
    mocks.progressDetail = {
      device: "/swap.img",
      index: 5,
      step: "remove_swap_file",
      total: 5,
    };
    mocks.tasks = [
      {
        id: "task-9",
        metadata: { action: "zram", identity: ["memory", "swap_mode"] },
        state: "running",
        type: "memory.set_swap_mode",
      },
    ];
    mocks.watchResult = swapModeResult(
      swapStatus({ devices: [zramDevice, swapFileDevice(GIB)], mode: "mixed" }),
      [{ path: "/swap.img", reason: "not_enough_memory", what: "swap_file" }],
    );
    await renderSection(diskStatus());

    expect(await screen.findByText("Removing /swap.img")).toBeVisible();
    expect(mocks.watched).toEqual(["task-9"]);
    expect(mocks.requests).toEqual([]);
    expect(radio("Compressed memory only")).toBeChecked();
    expect(radio("Disk only")).toBeDisabled();

    finish();
    await waitFor(() =>
      expect(mocks.toast.warning).toHaveBeenCalledWith(
        "Not enough free memory to bring back the data in /swap.img, so it stays on. Apply again when more memory is free.",
        undefined,
      ),
    );
  });

  it("takes up a drain that is already running when it opens", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.progress = "Moving swapped data back into memory";
    mocks.tasks = [
      { id: "task-7", state: "running", type: "memory.drain_swap" },
    ];
    mocks.watchResult = diskStatus();
    await renderSection(
      diskStatus({
        can_drain: true,
        devices: [swapFileDevice(GIB)],
        drain_blocked_reason: "",
      }),
    );

    expect(
      await screen.findByText("Moving swapped data back into memory"),
    ).toBeVisible();
    expect(mocks.watched).toEqual(["task-7"]);
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();

    finish();
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Parked data brought back to memory",
        undefined,
      ),
    );
  });

  it("leaves alone a job that has ended or names no mode", async () => {
    mocks.tasks = [
      { id: "task-1", state: "completed", type: "memory.set_swap_mode" },
      { id: "task-2", state: "running", type: "memory.set_swap_mode" },
    ];
    await renderSection(diskStatus());

    expect(mocks.watched).toEqual([]);
    expect(radio("Disk only")).toBeEnabled();
  });

  it("holds every control but Close while a drain runs", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.progress = "Moving swapped data back into memory";
    mocks.results["memory.drain_swap"] = diskStatus();
    const { user } = await renderSection(
      diskStatus({
        can_drain: true,
        devices: [swapFileDevice(GIB)],
        drain_blocked_reason: "",
      }),
    );
    await user.click(screen.getByRole("button", { name: "Advanced" }));
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });
    await user.click(radio("Compressed memory only"));

    await user.click(
      screen.getByRole("button", { name: "Bring back to memory" }),
    );

    expect(
      await screen.findByText("Moving swapped data back into memory"),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Bringing back…" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    expect(radio("Disk only")).toBeDisabled();
    expect(slider).toBeDisabled();

    finish();
    await waitFor(() =>
      expect(
        screen.queryByText("Moving swapped data back into memory"),
      ).not.toBeInTheDocument(),
    );
  });
});

describe("MemoryOverflowDialog swap files", () => {
  const MIB = 1024 ** 2;
  // The list under "Swap files".
  const swapList = () => screen.getByRole("list", { name: "Swap files" });
  const sizeField = () =>
    screen.getByRole("spinbutton", { name: "Size in MiB" });

  it("lists each swap file with its use and actions, then the other swap", async () => {
    await renderSection(
      diskStatus({
        devices: [
          swapFileDevice(GIB),
          swapFileDevice(0, "ext4", "/home/swapfile"),
          swapPartitionDevice(),
        ],
        swap_files: [
          swapFileRow({ used: GIB }),
          swapFileRow({
            locked_reason: "not_root_only",
            path: "/home/swapfile",
          }),
          swapFileRow({
            active: false,
            path: "/old.img",
            resize_blocked_reason: "off",
            size: 2 * GIB,
          }),
        ],
      }),
    );

    const list = swapList();
    expect(list).toHaveTextContent("/swap.img4 GB, 1 GB used");
    expect(
      screen.getByRole("button", { name: "Resize /swap.img" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Remove /swap.img" }),
    ).toBeEnabled();
    // A file LinuxIO does not act on says why and has no buttons.
    expect(list).toHaveTextContent(
      "LinuxIO does not resize or remove it: another user could put a file of their own at this name.",
    );
    expect(
      screen.queryByRole("button", { name: /\/home\/swapfile/ }),
    ).not.toBeInTheDocument();
    // A file in fstab that is off has Remove only, and says LinuxIO does not
    // turn it on.
    expect(list).toHaveTextContent("Off, 2 GB");
    expect(list).toHaveTextContent(
      "/etc/fstab names it, and LinuxIO does not turn it on.",
    );
    expect(
      screen.queryByRole("button", { name: "Resize /old.img" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Remove /old.img" }),
    ).toBeEnabled();
    // A partition is a plain row.
    expect(list).toHaveTextContent("/dev/vdb1Partition, 2 GB, none used");
    expect(
      screen.getByRole("button", { name: "Create a swap file" }),
    ).toBeEnabled();
  });

  it("says why LinuxIO cannot create its swap file", async () => {
    await renderSection(
      diskStatus({
        create: swapFileCreate({
          blocked_reason: "unsupported_filesystem",
          fs_type: "zfs",
        }),
      }),
    );

    expect(
      screen.getByText(
        "LinuxIO cannot create a swap file: /var/lib/linuxio/swap is on zfs, and LinuxIO makes swap files only on ext4, xfs and btrfs.",
      ),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Create a swap file" }),
    ).not.toBeInTheDocument();
  });

  it("resizes a swap file within the limits, and the step closes as the job starts", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.progress = "Resizing the swap file";
    mocks.progressDetail = {
      device: "/swap.img",
      index: 2,
      step: "resize_swap_file",
      total: 3,
    };
    mocks.results["memory.resize_swap_file"] = diskStatus();
    // Data is parked and the file can be moved, so the drain and Move are
    // there to be held too.
    const { user } = await renderSection(
      diskStatus({
        can_drain: true,
        devices: [swapFileDevice(GIB, "btrfs")],
        drain_blocked_reason: "",
        swap_files: [
          swapFileRow({
            blocks_snapshots: true,
            can_move: true,
            fs_type: "btrfs",
            used: GIB,
          }),
        ],
      }),
    );
    await user.click(screen.getByRole("button", { name: "Advanced" }));

    await user.click(screen.getByRole("button", { name: "Resize /swap.img" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Resize /swap.img",
    });
    expect(dialog).toHaveTextContent(
      "To change its size, LinuxIO turns the file off and on again. Data parked in it is brought back to memory first.",
    );
    expect(dialog).toHaveTextContent(
      "From 256 to 30720 MiB. 30720 MiB is the largest size the disk allows.",
    );
    expect(sizeField()).toHaveFocus();
    expect(sizeField()).toHaveValue(4096);
    // The current size changes nothing.
    const resize = screen.getByRole("button", { name: "Resize" });
    expect(resize).toBeDisabled();

    await user.clear(sizeField());
    await user.type(sizeField(), "30721");
    expect(dialog).toHaveTextContent(
      "At most 30720 MiB, the largest size the disk allows.",
    );
    expect(resize).toBeDisabled();
    await user.clear(sizeField());
    await user.type(sizeField(), "100");
    expect(dialog).toHaveTextContent("At least 256 MiB.");
    expect(resize).toBeDisabled();
    await user.clear(sizeField());
    await user.type(sizeField(), "8192");
    expect(resize).toBeEnabled();
    expect(mocks.requests).toEqual([]);

    await user.click(resize);

    expect(mocks.requests).toEqual([
      {
        request: { path: "/swap.img", size: 8192 * MIB },
        route: "memory.resize_swap_file",
      },
    ]);
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Resize /swap.img" }),
      ).not.toBeInTheDocument(),
    );
    // The first step holds its controls and shows the step, naming the file.
    expect(await screen.findByText("Resizing /swap.img")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Remove /swap.img" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Move /swap.img" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Create a swap file" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Bring back to memory" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("slider", {
        name: "Eagerness to move data out of memory",
      }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    expect(radio("Disk only")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Close" })).toHaveFocus();
    expect(screen.getByRole("button", { name: "Close" })).toBeEnabled();

    finish();
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Resized /swap.img",
        undefined,
      ),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Resize /swap.img" }),
      ).toBeEnabled(),
    );
  });

  it("starts a resize with Enter in the size field", async () => {
    mocks.results["memory.resize_swap_file"] = diskStatus();
    const { user } = await renderSection(diskStatus());

    await user.click(screen.getByRole("button", { name: "Resize /swap.img" }));
    await screen.findByRole("dialog", { name: "Resize /swap.img" });
    await user.clear(sizeField());
    await user.type(sizeField(), "2048{Enter}");

    await waitFor(() =>
      expect(mocks.requests).toEqual([
        {
          request: { path: "/swap.img", size: 2048 * MIB },
          route: "memory.resize_swap_file",
        },
      ]),
    );
  });

  it("goes back from a step to the button that opened it", async () => {
    const { user } = await renderSection(diskStatus());

    await user.click(screen.getByRole("button", { name: "Resize /swap.img" }));
    await screen.findByRole("dialog", { name: "Resize /swap.img" });
    await user.click(screen.getByRole("button", { name: "Back" }));

    expect(
      await screen.findByRole("dialog", { name: "Memory overflow" }),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Resize /swap.img" }),
    ).toHaveFocus();
    expect(mocks.requests).toEqual([]);
  });

  it("asks before removing a swap file, naming it and its size", async () => {
    mocks.results["memory.remove_swap_file"] = swapStatus();
    const { user } = await renderSection(
      diskStatus({ devices: [zramDevice, swapFileDevice()] }),
    );

    await user.click(screen.getByRole("button", { name: "Remove /swap.img" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Remove /swap.img?",
    });
    expect(dialog).toHaveTextContent(
      "/swap.img, 4 GB, is turned off, taken out of /etc/fstab and deleted.",
    );
    expect(dialog).toHaveTextContent(
      "Data parked in it is brought back to memory first.",
    );
    expect(dialog).not.toHaveTextContent("The host then has no swap.");
    expect(screen.getByRole("button", { name: "Back" })).toHaveFocus();
    expect(mocks.requests).toEqual([]);

    await user.click(screen.getByRole("button", { name: "Remove" }));

    expect(mocks.requests).toEqual([
      { request: { path: "/swap.img" }, route: "memory.remove_swap_file" },
    ]);
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Removed /swap.img",
        undefined,
      ),
    );
  });

  it("warns before removing the last swap", async () => {
    const { user } = await renderSection(diskStatus());

    await user.click(screen.getByRole("button", { name: "Remove /swap.img" }));

    expect(
      await screen.findByRole("dialog", { name: "Remove /swap.img?" }),
    ).toHaveTextContent(
      "The host then has no swap. If memory runs out, Linux will close programs.",
    );
  });

  it("creates LinuxIO's swap file at the size set", async () => {
    mocks.results["memory.create_swap_file"] = diskStatus();
    const { user } = await renderSection(swapStatus());

    await user.click(
      screen.getByRole("button", { name: "Create a swap file" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Create a swap file",
    });
    expect(dialog).toHaveTextContent(
      "LinuxIO creates the swap file /var/lib/linuxio/swap/swapfile, adds it to /etc/fstab and turns it on.",
    );
    expect(dialog).toHaveTextContent(
      "From 256 to 38912 MiB. 38912 MiB is the largest size the disk allows.",
    );
    expect(sizeField()).toHaveValue(8192);

    await user.click(screen.getByRole("button", { name: "Create" }));

    expect(mocks.requests).toEqual([
      { request: { size: 8192 * MIB }, route: "memory.create_swap_file" },
    ]);
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Swap file created",
        undefined,
      ),
    );
  });

  it("offers Move only with the btrfs line that says LinuxIO can make the file again", async () => {
    mocks.results["memory.move_swap_file"] = diskStatus();
    const { user } = await renderSection(
      diskStatus({
        devices: [swapFileDevice(0, "btrfs")],
        swap_files: [
          swapFileRow({
            blocks_snapshots: true,
            can_move: true,
            fs_type: "btrfs",
          }),
          swapFileRow({
            blocks_snapshots: true,
            fs_type: "btrfs",
            path: "/data/swapfile",
          }),
        ],
      }),
    );

    expect(swapList()).toHaveTextContent(
      "/swap.img blocks snapshots of the subvolume it is in. LinuxIO can make it again in a subvolume of its own, at the same size.",
    );
    expect(swapList()).toHaveTextContent(
      "/data/swapfile blocks snapshots of the subvolume it is in.",
    );
    expect(
      screen.queryByRole("button", { name: "Move /data/swapfile" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Move /swap.img" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Move /swap.img?",
    });
    expect(dialog).toHaveTextContent(
      "LinuxIO creates the swap file /var/lib/linuxio/swap/swapfile, 4 GB, in a subvolume of its own, adds it to /etc/fstab and turns it on.",
    );
    await user.click(screen.getByRole("button", { name: "Move" }));

    expect(mocks.requests).toEqual([
      { request: { path: "/swap.img" }, route: "memory.move_swap_file" },
    ]);
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Moved /swap.img into LinuxIO's folder",
        undefined,
      ),
    );
  });

  it("sets the size of the swap file a switch to disk creates", async () => {
    mocks.results["memory.set_swap_mode"] = swapModeResult(diskStatus());
    const { user } = await renderSection(swapStatus());

    expect(radio("Disk only")).toHaveAccessibleDescription(
      /Creates the swap file \/var\/lib\/linuxio\/swap\/swapfile first, at a size you choose\./,
    );
    await user.click(radio("Disk only"));
    await user.click(screen.getByRole("button", { name: "Apply" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Switch to disk only?",
    });
    expect(dialog).toHaveTextContent(
      "LinuxIO creates the swap file /var/lib/linuxio/swap/swapfile, adds it to /etc/fstab and turns it on.",
    );
    await user.clear(sizeField());
    await user.type(sizeField(), "4096");
    await user.click(screen.getByRole("button", { name: "Apply" }));

    expect(mocks.requests).toEqual([
      {
        request: { mode: "disk", swap_file_size: 4096 * MIB },
        route: "memory.set_swap_mode",
      },
    ]);
  });

  it.each([
    ["Remove /swap.img", "Remove", "Failed to remove /swap.img"],
    ["Resize /swap.img", "Resize", "Failed to resize /swap.img"],
    ["Move /swap.img", "Move", "Failed to move /swap.img"],
    ["Create a swap file", "Create", "Failed to create the swap file"],
  ])(
    "says which file a %s job failed on when the error has no message",
    async (opener, action, text) => {
      mocks.failWith = new Error("");
      const { user } = await renderSection(
        diskStatus({
          devices: [zramDevice, swapFileDevice(0, "btrfs")],
          swap_files: [
            swapFileRow({
              blocks_snapshots: true,
              can_move: true,
              fs_type: "btrfs",
            }),
          ],
        }),
      );

      await user.click(screen.getByRole("button", { name: opener }));
      await screen.findByRole("button", { name: "Back" });
      if (action === "Resize") {
        await user.clear(sizeField());
        await user.type(sizeField(), "8192");
      }
      await user.click(screen.getByRole("button", { name: action }));

      await waitFor(() =>
        expect(mocks.toast.error).toHaveBeenCalledWith(text, undefined),
      );
    },
  );

  it.each([
    ["a mode change", "memory.set_swap_mode"],
    ["a drain", "memory.drain_swap"],
  ])("holds the swap file buttons while %s runs", async (_name, route) => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.results[route] =
      route === "memory.set_swap_mode"
        ? swapModeResult(diskStatus({ mode: "zswap" }))
        : diskStatus();
    const { user } = await renderSection(
      diskStatus({
        can_drain: true,
        devices: [swapFileDevice(GIB, "btrfs")],
        drain_blocked_reason: "",
        swap_files: [
          swapFileRow({
            blocks_snapshots: true,
            can_move: true,
            fs_type: "btrfs",
            used: GIB,
          }),
        ],
      }),
    );

    if (route === "memory.set_swap_mode") {
      await user.click(radio("Compressed memory with the disk as backup"));
      await user.click(screen.getByRole("button", { name: "Apply" }));
    } else {
      await user.click(
        screen.getByRole("button", { name: "Bring back to memory" }),
      );
    }

    for (const name of [
      "Resize /swap.img",
      "Remove /swap.img",
      "Move /swap.img",
      "Create a swap file",
    ]) {
      await waitFor(() =>
        expect(screen.getByRole("button", { name })).toBeDisabled(),
      );
    }

    finish();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Resize /swap.img" }),
      ).toBeEnabled(),
    );
  });

  it("follows the host while a Resize step is open, and says when the file changed", async () => {
    const { queryClient, user } = await renderSection(diskStatus());
    // The status read again, as the 15-second refresh does.
    const refresh = async (status: SwapStatus) => {
      mocks.status = status;
      await act(() =>
        queryClient.invalidateQueries({
          queryKey: linuxio.memory.get_swap_status.queryKey,
        }),
      );
      await waitFor(() =>
        expect(
          queryClient.getQueryData(linuxio.memory.get_swap_status.queryKey),
        ).toEqual(status),
      );
    };

    await user.click(screen.getByRole("button", { name: "Resize /swap.img" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Resize /swap.img",
    });
    await user.clear(sizeField());
    await user.type(sizeField(), "20480");
    expect(screen.getByRole("button", { name: "Resize" })).toBeEnabled();

    // The disk filled up meanwhile: the limit follows it.
    await refresh(
      diskStatus({ swap_files: [swapFileRow({ max_size: 10 * GIB })] }),
    );
    expect(dialog).toHaveTextContent(
      "At most 10240 MiB, the largest size the disk allows.",
    );
    expect(screen.getByRole("button", { name: "Resize" })).toBeDisabled();

    // The file can no longer be resized.
    await refresh(
      diskStatus({
        swap_files: [swapFileRow({ resize_blocked_reason: "read_only" })],
      }),
    );
    expect(dialog).toHaveTextContent(
      "/swap.img changed while this was open, so go back to see what can be done with it now.",
    );
    expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Resize" })).toBeDisabled();
    expect(mocks.requests).toEqual([]);
  });

  it("says when the file of a Remove or Move step is gone or locked", async () => {
    const movable = swapFileRow({
      blocks_snapshots: true,
      can_move: true,
      fs_type: "btrfs",
    });
    const { queryClient, user } = await renderSection(
      diskStatus({
        devices: [zramDevice, swapFileDevice(0, "btrfs")],
        swap_files: [movable],
      }),
    );
    // The status read again, as the 15-second refresh does.
    const refresh = async (status: SwapStatus) => {
      mocks.status = status;
      await act(() =>
        queryClient.invalidateQueries({
          queryKey: linuxio.memory.get_swap_status.queryKey,
        }),
      );
      await waitFor(() =>
        expect(
          queryClient.getQueryData(linuxio.memory.get_swap_status.queryKey),
        ).toEqual(status),
      );
    };

    await user.click(screen.getByRole("button", { name: "Remove /swap.img" }));
    const remove = await screen.findByRole("dialog", {
      name: "Remove /swap.img?",
    });
    expect(remove).not.toHaveTextContent("The host then has no swap.");
    // zram went away: the file is now the last swap, and the step says so.
    await refresh(
      diskStatus({
        devices: [swapFileDevice(0, "btrfs")],
        swap_files: [movable],
      }),
    );
    expect(remove).toHaveTextContent(
      "The host then has no swap. If memory runs out, Linux will close programs.",
    );
    // The file is gone.
    await refresh(diskStatus({ swap_files: [] }));
    expect(remove).toHaveTextContent(
      "/swap.img changed while this was open, so go back to see what can be done with it now.",
    );
    expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Back" }));
    await refresh(
      diskStatus({
        devices: [zramDevice, swapFileDevice(0, "btrfs")],
        swap_files: [movable],
      }),
    );
    await user.click(
      await screen.findByRole("button", { name: "Move /swap.img" }),
    );
    const move = await screen.findByRole("dialog", { name: "Move /swap.img?" });
    expect(screen.getByRole("button", { name: "Move" })).toBeEnabled();
    // Move is no longer offered for it.
    await refresh(
      diskStatus({ swap_files: [{ ...movable, can_move: false }] }),
    );
    expect(move).toHaveTextContent(
      "/swap.img changed while this was open, so go back to see what can be done with it now.",
    );
    expect(screen.getByRole("button", { name: "Move" })).toBeDisabled();
    // Locked: nothing is offered.
    await refresh(
      diskStatus({
        swap_files: [{ ...movable, locked_reason: "hibernation" }],
      }),
    );
    expect(screen.getByRole("button", { name: "Move" })).toBeDisabled();
    expect(mocks.requests).toEqual([]);
  });

  it("reports a failed swap file job with the file, and not a cancelled one", async () => {
    mocks.failWith = new Error("not enough free memory");
    const { user } = await renderSection(
      diskStatus({ devices: [zramDevice, swapFileDevice()] }),
    );

    await user.click(screen.getByRole("button", { name: "Remove /swap.img" }));
    await user.click(await screen.findByRole("button", { name: "Remove" }));

    await waitFor(() =>
      expect(mocks.toast.error).toHaveBeenCalledWith(
        "not enough free memory",
        undefined,
      ),
    );

    mocks.toast.error.mockClear();
    mocks.failWith = new LinuxIOError("operation aborted", 499);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Remove /swap.img" }),
      ).toBeEnabled(),
    );
    await user.click(screen.getByRole("button", { name: "Remove /swap.img" }));
    await user.click(await screen.findByRole("button", { name: "Remove" }));
    await waitFor(() => expect(mocks.requests).toHaveLength(2));
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Remove /swap.img" }),
      ).toBeEnabled(),
    );
    expect(mocks.toast.error).not.toHaveBeenCalled();
  });

  it("takes up a swap file job that is already running when it opens", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.progress = "Adding a temporary swap file";
    mocks.progressDetail = {
      device: "/swap.img.linuxio-resize",
      index: 1,
      step: "make_temporary_swap",
      total: 3,
    };
    mocks.tasks = [
      {
        id: "task-5",
        metadata: {
          action: "resize",
          identity: ["memory", "swap_file"],
          path: "/swap.img",
        },
        state: "running",
        type: "memory.resize_swap_file",
      },
    ];
    mocks.watchResult = diskStatus();
    await renderSection(diskStatus());

    expect(
      await screen.findByText(
        "Adding the temporary swap file /swap.img.linuxio-resize",
      ),
    ).toBeVisible();
    expect(mocks.watched).toEqual(["task-5"]);
    expect(
      screen.getByRole("button", { name: "Resize /swap.img" }),
    ).toBeDisabled();

    finish();
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Resized /swap.img",
        undefined,
      ),
    );
  });
});

describe("MemoryOverflowDialog close", () => {
  it("closes from its button and forgets an unapplied choice", async () => {
    const { user } = await renderSection(diskStatus());
    await user.click(radio("Compressed memory only"));

    await user.click(screen.getByRole("button", { name: "Close" }));

    expect(mocks.onClose).toHaveBeenCalledTimes(1);
    expect(radio("Disk only")).toBeChecked();
    expect(mocks.requests).toEqual([]);
  });
});

describe("MemoryOverflowDialog advanced", () => {
  const openAdvanced = async (status: SwapStatus) => {
    const view = await renderSection(status);
    expect(
      screen.queryByRole("slider", { name: /Eagerness/ }),
    ).not.toBeInTheDocument();
    await view.user.click(screen.getByRole("button", { name: "Advanced" }));
    return view;
  };

  it("is collapsed until opened, then shows the host's value and the preset", async () => {
    await openAdvanced(diskStatus());

    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });
    expect(slider).toHaveValue("60");
    expect(slider).toHaveAttribute("min", "0");
    expect(slider).toHaveAttribute("max", "200");
    expect(slider).toHaveAccessibleDescription("Recommended for Disk only: 60");
  });

  it("follows the selected row in its caption", async () => {
    const { user } = await openAdvanced(diskStatus());

    await user.click(radio("Compressed memory only"));

    expect(
      await screen.findByText("Recommended for Compressed memory only: 180"),
    ).toBeVisible();
  });

  it("saves only when the user lets go", async () => {
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 100 });
    await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    fireEvent.change(slider, { target: { value: "90" } });
    fireEvent.change(slider, { target: { value: "100" } });
    expect(mocks.requests).toEqual([]);

    fireEvent.pointerUp(slider);
    await waitFor(() =>
      expect(mocks.requests).toEqual([
        { request: { swappiness: 100 }, route: "memory.set_swappiness" },
      ]),
    );
    await waitFor(() =>
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Eagerness updated",
        undefined,
      ),
    );
    expect(slider).toHaveValue("100");
  });

  it("keeps focus on the slider while its own save runs, then sends the newer value once", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 61 });
    await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    vi.useFakeTimers({ shouldAdvanceTime: true });
    slider.focus();
    fireEvent.change(slider, { target: { value: "61" } });
    fireEvent.keyUp(slider, { key: "ArrowRight" });
    await passKeyWait();

    await waitFor(() => expect(mocks.requests).toHaveLength(1));
    expect(slider).toBeEnabled();
    expect(slider).toHaveFocus();

    // Another step and release while the save runs: no second request.
    fireEvent.change(slider, { target: { value: "62" } });
    fireEvent.keyUp(slider, { key: "ArrowRight" });
    expect(mocks.requests).toHaveLength(1);
    expect(slider).toBeEnabled();
    expect(slider).toHaveFocus();
    expect(slider).toHaveValue("62");

    // The save ends; the step made meanwhile is sent without another key
    // press, as the second and last request.
    mocks.hold = null;
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 62 });
    finish();
    await waitFor(() => expect(mocks.requests).toHaveLength(2));
    expect(mocks.requests[1]).toEqual({
      request: { swappiness: 62 },
      route: "memory.set_swappiness",
    });
    await waitFor(() => expect(slider).toHaveValue("62"));
    // The key released during the save does not send it a second time.
    await passKeyWait();
    expect(mocks.requests).toHaveLength(2);
  });

  it("saves a run of arrow keys once", async () => {
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 63 });
    await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    vi.useFakeTimers({ shouldAdvanceTime: true });
    for (const value of ["61", "62", "63"]) {
      fireEvent.change(slider, { target: { value } });
      fireEvent.keyUp(slider, { key: "ArrowRight" });
      await act(() => vi.advanceTimersByTimeAsync(400));
    }
    expect(mocks.requests).toEqual([]);

    await passKeyWait();
    await waitFor(() =>
      expect(mocks.requests).toEqual([
        { request: { swappiness: 63 }, route: "memory.set_swappiness" },
      ]),
    );
    await waitFor(() => expect(mocks.toast.success).toHaveBeenCalledTimes(1));
  });

  it("saves a key press at once when focus leaves the slider", async () => {
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 61 });
    await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.change(slider, { target: { value: "61" } });
    fireEvent.keyUp(slider, { key: "ArrowRight" });
    fireEvent.blur(slider);
    await waitFor(() => expect(mocks.requests).toHaveLength(1));

    await passKeyWait();
    expect(mocks.requests).toEqual([
      { request: { swappiness: 61 }, route: "memory.set_swappiness" },
    ]);
  });

  it("does not start the wait for a key that does not move the slider", async () => {
    await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.change(slider, { target: { value: "61" } });
    fireEvent.keyUp(slider, { key: "Shift" });
    await passKeyWait();
    expect(mocks.requests).toEqual([]);
  });

  it("saves a key press that is still waiting when Escape closes the dialog", async () => {
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 61 });
    await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    fireEvent.change(slider, { target: { value: "61" } });
    fireEvent.keyUp(slider, { key: "ArrowRight" });
    expect(mocks.requests).toEqual([]);
    fireEvent.keyDown(slider, { key: "Escape" });

    await waitFor(() =>
      expect(mocks.requests).toEqual([
        { request: { swappiness: 61 }, route: "memory.set_swappiness" },
      ]),
    );
    expect(mocks.onClose).toHaveBeenCalledTimes(1);
  });

  it("sends a value the user went back to while a save ran", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 70 });
    await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    fireEvent.change(slider, { target: { value: "70" } });
    fireEvent.pointerUp(slider);
    await waitFor(() => expect(mocks.requests).toHaveLength(1));

    // Back to the host's current value, released during the save.
    fireEvent.change(slider, { target: { value: "60" } });
    fireEvent.pointerUp(slider);

    mocks.hold = null;
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 60 });
    finish();
    await waitFor(() => expect(mocks.requests).toHaveLength(2));
    expect(mocks.requests[1].request).toEqual({ swappiness: 60 });
  });

  it("does not retry after a failed save and goes back to the host's value", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.failWith = new Error("the kernel refused it");
    await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    fireEvent.change(slider, { target: { value: "61" } });
    fireEvent.pointerUp(slider);
    await waitFor(() => expect(mocks.requests).toHaveLength(1));
    fireEvent.change(slider, { target: { value: "62" } });
    fireEvent.pointerUp(slider);

    finish();
    await waitFor(() =>
      expect(mocks.toast.error).toHaveBeenCalledWith(
        "the kernel refused it",
        undefined,
      ),
    );
    expect(mocks.requests).toHaveLength(1);
    await waitFor(() => expect(slider).toHaveValue("60"));
  });

  it("still sends a step made during a save when the dialog is closed", async () => {
    let finish: () => void = () => {};
    mocks.hold = new Promise<void>((resolve) => {
      finish = resolve;
    });
    mocks.results["memory.set_swap_mode"] = null;
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 40 });
    const { user } = await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    fireEvent.change(slider, { target: { value: "40" } });
    fireEvent.pointerUp(slider);
    await waitFor(() => expect(mocks.requests).toHaveLength(1));
    fireEvent.change(slider, { target: { value: "70" } });
    fireEvent.pointerUp(slider);
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(mocks.onClose).toHaveBeenCalledTimes(1);

    mocks.hold = null;
    mocks.results["memory.set_swappiness"] = diskStatus({ swappiness: 70 });
    finish();
    await waitFor(() => expect(mocks.requests).toHaveLength(2));
    expect(mocks.requests[1].request).toEqual({ swappiness: 70 });
  });

  it("lets the slider follow the host again after a drag back to its value", async () => {
    const { queryClient } = await openAdvanced(diskStatus());
    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });

    fireEvent.change(slider, { target: { value: "70" } });
    fireEvent.change(slider, { target: { value: "60" } });
    fireEvent.pointerUp(slider);
    expect(mocks.requests).toEqual([]);

    mocks.status = diskStatus({ swappiness: 90 });
    await act(async () => {
      await queryClient.invalidateQueries({
        queryKey: linuxio.memory.get_swap_status.queryKey,
      });
    });

    await waitFor(() => expect(slider).toHaveValue("90"));
  });

  it("is read-only while TuneD manages it", async () => {
    await openAdvanced(
      diskStatus({
        tuned_active: true,
        tuned_profile: "throughput-performance",
      }),
    );

    const slider = await screen.findByRole("slider", {
      name: "Eagerness to move data out of memory",
    });
    expect(slider).toBeDisabled();
    expect(slider).toHaveAccessibleDescription(
      "Managed by the TuneD profile throughput-performance",
    );
  });
});
