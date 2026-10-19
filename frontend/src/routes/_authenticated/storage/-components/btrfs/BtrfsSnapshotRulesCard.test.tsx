import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  BtrfsSnapshotRule,
  BtrfsSnapshotRules,
  BtrfsSubvolume,
} from "@/api";
import { render } from "@/test/render";

import BtrfsSnapshotRulesCard from "./BtrfsSnapshotRulesCard";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
  rules: vi.fn(),
  subvolumes: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: (endpoint: { route: string }) => ({
      isPending: false,
      mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
    }),
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_subvolumes: (request: { uuid: string }) => ({
          queryKey: ["test-btrfs-subvolumes", request.uuid],
          queryFn: () => mocks.subvolumes(request),
        }),
        btrfs_snapshot_rules: {
          queryKey: ["test-btrfs-snapshot-rules"],
          queryFn: () => mocks.rules(),
        },
      },
    },
  };
});

const subvolume = (overrides: Partial<BtrfsSubvolume>): BtrfsSubvolume => ({
  id: 256,
  parentId: 5,
  path: "@",
  uuid: "u-256",
  parentUuid: "",
  receivedUuid: "",
  readOnly: false,
  isDefault: false,
  isSnapshot: false,
  ...overrides,
});

const root = subvolume({});
const data = subvolume({ id: 257, path: "data", uuid: "u-257" });
const snapshot = subvolume({
  id: 300,
  path: "linuxio-snapshots/@-1",
  isSnapshot: true,
});

const dataRule: BtrfsSnapshotRule = {
  uuid: "uuid-1",
  path: "data",
  enabled: true,
  every: "weekly",
  hour: 4,
  weekday: 1,
  keep: { hourly: 0, daily: 3, weekly: 2, monthly: 1 },
  beforeUpdates: false,
};

const route = (name: string) => {
  const mutate = mocks.mutations[`storage.${name}`];
  if (!mutate) throw new Error(`no mutation registered for ${name}`);
  return mutate;
};

const rowFor = async (path: string) =>
  (await screen.findByText(path)).closest("[role=row]") as HTMLElement;

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.subvolumes.mockResolvedValue([root, data, snapshot]);
  mocks.rules.mockResolvedValue({
    rules: [dataRule],
    timerEnabled: true,
  } satisfies BtrfsSnapshotRules);
});

describe("BtrfsSnapshotRulesCard", () => {
  it("summarises each rule and shows Off without one", async () => {
    render(<BtrfsSnapshotRulesCard uuid="uuid-1" />);

    expect(within(await rowFor("@")).getByText("Off")).toBeInTheDocument();
    const dataRow = await rowFor("data");
    expect(
      within(dataRow).getByText("Every week on Monday at 04:00"),
    ).toBeInTheDocument();
    expect(within(dataRow).getByText("0h · 3d · 2w · 1m")).toBeInTheDocument();
    expect(screen.queryByText("linuxio-snapshots/@-1")).not.toBeInTheDocument();
    expect(await screen.findByText("Timer enabled")).toBeInTheDocument();
  });

  it("creates a rule from the edit dialog", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotRulesCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("@")).getByRole("button", { name: "Edit" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).queryByRole("button", { name: "Remove rule" }),
    ).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("checkbox", { name: "Enabled" }));
    expect(
      within(dialog).queryByRole("combobox", { name: "At" }),
    ).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("combobox", { name: "Every" }));
    await user.click(screen.getByRole("option", { name: "Daily" }));
    expect(
      within(dialog).queryByRole("combobox", { name: "On" }),
    ).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("combobox", { name: "At" }));
    await user.click(screen.getByRole("option", { name: "22:00" }));
    await user.click(
      within(dialog).getByRole("checkbox", { name: /Snapshot before updates/ }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(route("btrfs_snapshot_rule_set")).toHaveBeenCalledWith(
      {
        rule: {
          uuid: "uuid-1",
          path: "@",
          enabled: true,
          every: "daily",
          hour: 22,
          weekday: 0,
          keep: { hourly: 24, daily: 7, weekly: 4, monthly: 6 },
          beforeUpdates: true,
        },
      },
      expect.anything(),
    );
  });

  it("blocks saving an enabled rule that keeps nothing and truncates keeps", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotRulesCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("data")).getByRole("button", { name: "Edit" }),
    );
    const dialog = await screen.findByRole("dialog");
    for (const name of ["Keep daily", "Keep weekly", "Keep monthly"]) {
      fireEvent.change(within(dialog).getByRole("spinbutton", { name }), {
        target: { value: "0" },
      });
    }
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
    expect(
      within(dialog).getByText(
        "0 = keep none of this bucket; an enabled rule needs at least one",
      ),
    ).toBeInTheDocument();

    fireEvent.change(
      within(dialog).getByRole("spinbutton", { name: "Keep hourly" }),
      { target: { value: "2.7" } },
    );
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(route("btrfs_snapshot_rule_set")).toHaveBeenCalledWith(
      {
        rule: {
          ...dataRule,
          keep: { hourly: 2, daily: 0, weekly: 0, monthly: 0 },
        },
      },
      expect.anything(),
    );
  });

  it("removes an existing rule", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotRulesCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("data")).getByRole("button", { name: "Edit" }),
    );
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Remove rule",
      }),
    );

    expect(route("btrfs_snapshot_rule_delete")).toHaveBeenCalledWith(
      { uuid: "uuid-1", path: "data" },
      expect.anything(),
    );
  });

  it("turns automatic snapshots on with the default rule", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotRulesCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("@")).getByRole("checkbox", {
        name: "Automatic snapshots of @",
      }),
    );

    expect(route("btrfs_snapshot_rule_set")).toHaveBeenCalledWith({
      rule: {
        uuid: "uuid-1",
        path: "@",
        enabled: true,
        every: "hourly",
        hour: 3,
        weekday: 0,
        keep: { hourly: 24, daily: 7, weekly: 4, monthly: 6 },
        beforeUpdates: false,
      },
    });
  });

  it("turns an existing rule off without losing its settings", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotRulesCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("data")).getByRole("checkbox", {
        name: "Automatic snapshots of data",
      }),
    );

    expect(route("btrfs_snapshot_rule_set")).toHaveBeenCalledWith({
      rule: { ...dataRule, enabled: false },
    });
  });

  it("runs the timeline now", async () => {
    const user = userEvent.setup();
    render(<BtrfsSnapshotRulesCard uuid="uuid-1" />);
    await rowFor("@");

    await user.click(screen.getByRole("button", { name: "Run now" }));

    expect(route("btrfs_snapshot_run_now")).toHaveBeenCalled();
  });
});
