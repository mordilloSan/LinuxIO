import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsRollbackResult, BtrfsSnapshot } from "@/api";
import { render } from "@/test/render";
import type { TaskStreamActionMockConfig } from "@/test/taskStreamAction";

import BtrfsSnapshotsCard from "./BtrfsSnapshotsCard";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
  navigate: vi.fn(),
  rollbackResult: undefined as unknown,
  snapshots: vi.fn(),
  submit: vi.fn(),
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
  const { createTaskStreamActionMock } =
    await import("@/test/taskStreamAction");
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
        btrfs_snapshots: (request: { uuid: string }) => ({
          queryKey: ["test-btrfs-snapshots", request.uuid],
          queryFn: () => mocks.snapshots(request),
        }),
        btrfs_snapshot_rollback: {
          useTaskStreamAction: (config?: TaskStreamActionMockConfig) => ({
            isPending: false,
            ...createTaskStreamActionMock(
              {
                openStream: () => ({}),
                runStream: () => Promise.resolve(mocks.rollbackResult),
                submit: mocks.submit,
              },
              config,
            ),
          }),
        },
      },
    },
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

const route = (name: string) => {
  const mutate = mocks.mutations[`storage.${name}`];
  if (!mutate) throw new Error(`no mutation registered for ${name}`);
  return mutate;
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.rollbackResult = { rebootRequired: false, remounted: true };
  mocks.submit.mockResolvedValue({ id: "task-1" });
  mocks.snapshots.mockResolvedValue([manual, timeline, dataSnapshot]);
});

const rows = async () => {
  await screen.findAllByRole("button", { name: "Browse" });
  return screen.getAllByRole("row").slice(1);
};

describe("BtrfsSnapshotsCard", () => {
  it("renders the snapshot kinds as chips", async () => {
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    expect(await screen.findByText("manual")).toBeInTheDocument();
    expect(screen.getByText("timeline")).toBeInTheDocument();
    expect(screen.getByText("pre-update")).toBeInTheDocument();
    expect(mocks.snapshots).toHaveBeenCalledWith({ uuid: "uuid-1" });
  });

  it("narrows the rows with the source filter", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);
    expect(await rows()).toHaveLength(3);

    await user.click(screen.getByRole("combobox", { name: "Source" }));
    await user.click(screen.getByRole("option", { name: "data" }));

    expect(await rows()).toHaveLength(1);
    expect(screen.queryByText("timeline")).not.toBeInTheDocument();
  });

  it("browses a snapshot in the file browser", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    await user.click(
      within((await rows())[0]).getByRole("button", { name: "Browse" }),
    );

    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/filebrowser/$",
      params: { _splat: `run/linuxio/btrfs/uuid-1/${manual.path}` },
    });
  });

  it("confirms before deleting", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    await user.click(
      within((await rows())[0]).getByRole("button", { name: "Delete" }),
    );
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

  it("submits the rollback and shows the outcome", async () => {
    const user = userEvent.setup();
    mocks.rollbackResult = {
      remounted: false,
      rebootRequired: false,
      leftoverPath: "@.old",
      skippedMounts: ["/var/www"],
    } satisfies BtrfsRollbackResult;
    render(<BtrfsSnapshotsCard uuid="uuid-1" />);

    await user.click(
      within((await rows())[0]).getByRole("button", { name: "Roll back" }),
    );
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

    await user.click(
      within((await rows())[0]).getByRole("button", { name: "Roll back" }),
    );
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Roll back" }));

    await user.click(
      await within(dialog).findByRole("button", { name: "Reboot now" }),
    );
    expect(mocks.triggerReboot).toHaveBeenCalled();
    expect(mocks.mutations["control.reboot"]).toHaveBeenCalled();
  });
});
