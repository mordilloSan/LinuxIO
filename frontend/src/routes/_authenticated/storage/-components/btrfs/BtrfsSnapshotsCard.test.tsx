import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsRollbackResult, BtrfsSnapshot, BtrfsSubvolume } from "@/api";
import { LinuxIOError } from "@/api/errors";
import { render } from "@/test/render";

import BtrfsSnapshotsCard from "./BtrfsSnapshotsCard";
import { SNAPSHOT_DAY_FORMAT } from "./snapshotDays";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  configs: {} as Record<
    string,
    { error?: (error: unknown) => void } | undefined
  >,
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
  navigate: vi.fn(),
  rollbackResult: undefined as unknown,
  settlePowerAction: vi.fn(),
  snapshots: vi.fn(),
  submit: vi.fn(),
  subvolumes: vi.fn(),
  triggerReboot: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/hooks/usePowerAction", () => ({
  default: () => ({
    triggerPowerOff: vi.fn(),
    triggerReboot: mocks.triggerReboot,
  }),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { createMutationJobMock } = await import("@/test/mutationJob");
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }) => ({
    isPending: false,
    mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
  });
  // Rollback is a job; the rest answer from the call mock.
  const useMutation = (endpoint: { route: string }, config: never) => {
    mocks.configs[endpoint.route] = config;
    const call = useCallMutation(endpoint);
    if (endpoint.route !== "storage.btrfs_snapshot_rollback") return call;
    return {
      isPending: false,
      ...createMutationJobMock(
        {
          follow: () => Promise.resolve(mocks.rollbackResult),
          start: mocks.submit,
        },
        config,
      ),
    };
  };
  return {
    ...actual,
    call: (route: string) =>
      route === "tasks.list" ? Promise.resolve([]) : Promise.reject(),
    useCallMutation,
    linuxio: mockMutationHooks(
      {
        ...actual.linuxio,
        storage: {
          ...actual.linuxio.storage,
          btrfs_snapshots: (request: { uuid: string }) => ({
            queryKey: ["test-btrfs-snapshots", request.uuid],
            queryFn: () => mocks.snapshots(request),
          }),
          btrfs_subvolumes: (request: { uuid: string }) => ({
            queryKey: ["test-btrfs-subvolumes", request.uuid],
            queryFn: () => mocks.subvolumes(request),
          }),
          btrfs_snapshot_rules: {
            queryKey: ["test-btrfs-snapshot-rules"],
            queryFn: () => Promise.resolve({ rules: [], timerEnabled: false }),
          },
        },
      },
      useMutation,
    ),
  };
});

const snapshot = (overrides: Partial<BtrfsSnapshot>): BtrfsSnapshot => ({
  source: "@",
  path: "linuxio-snapshots/@/20261019T100000Z-manual",
  kind: "manual",
  createdAt: "2026-10-19T10:00:00Z",
  readOnly: true,
  id: 300,
  uuid: "u-300",
  receivedUuid: "",
  ...overrides,
});

const manual = snapshot({});
const timeline = snapshot({
  path: "linuxio-snapshots/@/20261019T110000Z-timeline",
  kind: "timeline",
  createdAt: "2026-10-19T11:00:00Z",
  id: 301,
});
const dataSnapshot = snapshot({
  source: "data",
  path: "linuxio-snapshots/data/20261019T120000Z-pre-update",
  kind: "pre-update",
  createdAt: "2026-10-19T12:00:00Z",
  id: 302,
});

const subvolume = (path: string, id: number): BtrfsSubvolume => ({
  id,
  parentId: 5,
  path,
  uuid: `u-${id}`,
  parentUuid: "",
  receivedUuid: "",
  readOnly: false,
  isDefault: false,
  isSnapshot: false,
});

const route = (name: string) => {
  const mutate = mocks.mutations[`storage.${name}`];
  if (!mutate) throw new Error(`no mutation registered for ${name}`);
  return mutate;
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.configs = {};
  mocks.triggerReboot.mockReturnValue(mocks.settlePowerAction);
  mocks.rollbackResult = { rebootRequired: false, remounted: true };
  mocks.submit.mockResolvedValue({ id: "task-1" });
  mocks.snapshots.mockResolvedValue([manual, timeline, dataSnapshot]);
  mocks.subvolumes.mockResolvedValue([
    subvolume("@", 256),
    subvolume("data", 257),
    subvolume("var/lib/portables", 258),
  ]);
});

const rows = async () =>
  within(
    await screen.findByRole("region", { name: "Btrfs snapshots" }),
  ).findAllByRole("listitem");

// Newest first: data 12:00, timeline 11:00, manual 10:00.
const manualRow = async () => (await rows())[2];

const panel = () => screen.getByRole("region", { name: "Selected snapshot" });

/** Selects the row's snapshot, then presses an action of the panel. */
const chooseFromMenu = async (
  user: ReturnType<typeof userEvent.setup>,
  row: HTMLElement,
  action: string,
) => {
  await user.click(within(row).getByRole("button"));
  await user.click(within(panel()).getByRole("button", { name: action }));
};

describe("BtrfsSnapshotsCard", () => {
  it("renders the snapshot kinds in words", async () => {
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    const [data, scheduled, manualItem] = await rows();
    expect(data).toHaveTextContent("Before an update");
    expect(scheduled).toHaveTextContent("Scheduled");
    expect(manualItem).toHaveTextContent("Taken by hand");
    expect(mocks.snapshots).toHaveBeenCalledWith({ uuid: "uuid-1" });
  });

  it("groups snapshots under the day they were taken", async () => {
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    expect(
      await screen.findByRole("list", {
        name: new Intl.DateTimeFormat(undefined, SNAPSHOT_DAY_FORMAT).format(
          new Date(manual.createdAt),
        ),
      }),
    ).toBeInTheDocument();
  });

  it("narrows the rows to one kind", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    expect(await rows()).toHaveLength(3);

    const show = screen.getByRole("group", { name: "Show" });
    await user.click(within(show).getByRole("button", { name: "By hand" }));

    const remaining = await rows();
    expect(remaining).toHaveLength(1);
    expect(remaining[0]).toHaveTextContent("Taken by hand");
    expect(
      within(show).getByRole("button", { name: "By hand" }),
    ).toHaveAttribute("aria-pressed", "true");
  });

  it("says why the selected snapshot is kept", async () => {
    const user = userEvent.setup();
    mocks.snapshots.mockResolvedValue([
      { ...timeline, keptBy: "daily" },
      { ...manual, keptBy: "manual" },
    ]);
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    const [scheduled] = await rows();
    expect(panel()).toHaveTextContent("Select a snapshot");

    await user.click(within(scheduled).getByRole("button"));

    expect(within(scheduled).getByRole("button")).toHaveAttribute(
      "aria-current",
      "true",
    );
    expect(panel()).toHaveTextContent("The schedule keeps it for its day");
    expect(
      within(panel()).getByRole("button", { name: "Roll back…" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^More actions/ }),
    ).not.toBeInTheDocument();
  });

  it("names each row's subvolume only when there are several", async () => {
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    expect((await rows())[0]).toHaveTextContent("data");

    cleanup();
    mocks.snapshots.mockResolvedValue([manual, timeline]);
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    const [first] = await rows();
    expect(within(first).queryByText("@")).not.toBeInTheDocument();
  });

  it("narrows the rows with the source filter", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    expect(await rows()).toHaveLength(3);

    await user.click(screen.getByRole("combobox", { name: "Source" }));
    await user.click(screen.getByRole("option", { name: "data" }));

    const remaining = await rows();
    expect(remaining).toHaveLength(1);
    expect(remaining[0]).toHaveTextContent("Before an update");
  });

  it("browses a snapshot in the file browser", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    await chooseFromMenu(user, await manualRow(), "Browse files");

    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/filebrowser/$",
      params: { _splat: `run/linuxio/btrfs/uuid-1/${manual.path}` },
    });
  });

  it("confirms before deleting", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    await chooseFromMenu(user, await manualRow(), "Delete…");
    expect(route("btrfs_snapshot_delete")).not.toHaveBeenCalled();
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Delete",
      }),
    );

    expect(route("btrfs_snapshot_delete")).toHaveBeenCalledWith(
      { uuid: "uuid-1", snapshotPath: manual.path },
      expect.anything(),
    );
  });

  it("takes a snapshot of a chosen subvolume", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    await rows();

    await user.click(screen.getByRole("button", { name: "Take snapshot" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByRole("button", { name: "Take snapshot" }),
    ).toBeDisabled();
    await user.click(
      within(dialog).getByRole("combobox", { name: "Subvolume" }),
    );
    expect(
      screen.queryByRole("option", { name: "var/lib/portables" }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("option", { name: "data" }));
    await user.click(
      within(dialog).getByRole("button", { name: "Take snapshot" }),
    );

    expect(route("btrfs_snapshot_create")).toHaveBeenCalledWith(
      { uuid: "uuid-1", path: "data" },
      expect.anything(),
    );
  });

  it("skips the picker when there is one subvolume", async () => {
    const user = userEvent.setup();
    mocks.subvolumes.mockResolvedValue([subvolume("@", 256)]);
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    await rows();

    await user.click(screen.getByRole("button", { name: "Take snapshot" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).queryByRole("combobox", { name: "Subvolume" }),
    ).not.toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "Take snapshot" }),
    );

    expect(route("btrfs_snapshot_create")).toHaveBeenCalledWith(
      { uuid: "uuid-1", path: "@" },
      expect.anything(),
    );
  });

  it("submits the rollback and shows the outcome", async () => {
    const user = userEvent.setup();
    mocks.rollbackResult = {
      remounted: false,
      rebootRequired: false,
      leftoverPath: "@.old",
      skippedMounts: ["/var/www"],
    } satisfies BtrfsRollbackResult;
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    await chooseFromMenu(user, await manualRow(), "Roll back…");
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText("Roll back @?", { exact: false }),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Roll back" }));

    expect(mocks.submit).toHaveBeenCalledWith({
      uuid: "uuid-1",
      snapshotPath: manual.path,
    });
    expect(
      await within(dialog).findByText(
        "The previous contents were kept at @.old.",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByText(
        "These mounts show a path inside @ and were left untouched: /var/www. Remount them or reboot to see the restored contents.",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).queryByText("Changes take effect after the next mount."),
    ).not.toBeInTheDocument();
    expect(
      within(dialog).queryByRole("button", { name: "Reboot now" }),
    ).not.toBeInTheDocument();
  });

  it("offers a reboot when the rollback needs one", async () => {
    const user = userEvent.setup();
    mocks.rollbackResult = { remounted: false, rebootRequired: true };
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    await chooseFromMenu(user, await manualRow(), "Roll back…");
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Roll back" }));

    await user.click(
      await within(dialog).findByRole("button", { name: "Reboot now" }),
    );
    expect(mocks.triggerReboot).toHaveBeenCalled();
    expect(mocks.mutations["control.reboot"]).toHaveBeenCalled();
  });

  it("hands a refused reboot to the power overlay", async () => {
    const user = userEvent.setup();
    mocks.rollbackResult = { remounted: false, rebootRequired: true };
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    await chooseFromMenu(user, await manualRow(), "Roll back…");
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Roll back" }));
    await user.click(
      await within(dialog).findByRole("button", { name: "Reboot now" }),
    );

    const refusal = new LinuxIOError("access denied", 403);
    mocks.configs["control.reboot"]?.error?.(refusal);
    expect(mocks.settlePowerAction).toHaveBeenCalledWith(refusal);
  });

  it("leaves out why a snapshot is kept when that is not known", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    await user.click(within(await manualRow()).getByRole("button"));
    expect(panel()).not.toHaveTextContent("Kept");
  });

  it("says the replication parent is one of the two newest", async () => {
    const user = userEvent.setup();
    mocks.snapshots.mockResolvedValue([{ ...timeline, keptBy: "replication" }]);
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    const [row] = await rows();
    await user.click(within(row).getByRole("button"));
    expect(panel()).toHaveTextContent(
      "One of the two newest, kept for copying to another pool or server",
    );
  });

  it("shows no panel without snapshots", async () => {
    mocks.snapshots.mockResolvedValue([]);
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    expect(await screen.findByText("No snapshots yet")).toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "Selected snapshot" }),
    ).not.toBeInTheDocument();
  });
});
