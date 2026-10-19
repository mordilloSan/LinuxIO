import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsSubvolume } from "@/api";
import { render } from "@/test/render";

import BtrfsSubvolumesCard from "./BtrfsSubvolumesCard";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  mutations: {} as Record<string, ReturnType<typeof vi.fn>>,
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

const root = subvolume({ isDefault: true, mountpoint: "/" });
const data = subvolume({ id: 257, path: "data", uuid: "u-257" });
const snapshot = subvolume({
  id: 300,
  path: "linuxio-snapshots/@-1",
  isSnapshot: true,
});

const route = (name: string) => {
  const mutate = mocks.mutations[`storage.${name}`];
  if (!mutate) throw new Error(`no mutation registered for ${name}`);
  return mutate;
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.subvolumes.mockResolvedValue([root, data, snapshot]);
});

const rowFor = async (path: string) =>
  (await screen.findByText(path)).closest("[role=row]") as HTMLElement;

describe("BtrfsSubvolumesCard", () => {
  it("lists subvolumes without snapshots and guards the default row", async () => {
    render(<BtrfsSubvolumesCard uuid="uuid-1" />);

    const rootRow = await rowFor("@");
    expect(screen.queryByText("linuxio-snapshots/@-1")).not.toBeInTheDocument();
    expect(within(rootRow).getByText("Default")).toBeInTheDocument();
    expect(
      within(rootRow).getByRole("button", { name: "Delete" }),
    ).toBeDisabled();
    expect(
      within(rootRow).getByRole("button", { name: "Set default" }),
    ).toBeDisabled();

    const dataRow = await rowFor("data");
    expect(
      within(dataRow).getByRole("button", { name: "Delete" }),
    ).toBeEnabled();
    expect(mocks.subvolumes).toHaveBeenCalledWith({ uuid: "uuid-1" });
  });

  it("creates a subvolume from the dialog", async () => {
    const user = userEvent.setup();
    render(<BtrfsSubvolumesCard uuid="uuid-1" />);
    await screen.findByText("data");

    await user.click(screen.getByRole("button", { name: "Create subvolume" }));
    await user.type(
      screen.getByRole("textbox", { name: "Subvolume path" }),
      "data/photos",
    );
    await user.click(screen.getByRole("button", { name: "Create" }));

    expect(route("btrfs_subvolume_create")).toHaveBeenCalledWith(
      { uuid: "uuid-1", path: "data/photos" },
      expect.anything(),
    );
  });

  it("confirms before deleting", async () => {
    const user = userEvent.setup();
    render(<BtrfsSubvolumesCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("data")).getByRole("button", { name: "Delete" }),
    );
    expect(route("btrfs_subvolume_delete")).not.toHaveBeenCalled();
    await user.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Delete",
      }),
    );

    expect(route("btrfs_subvolume_delete")).toHaveBeenCalledWith(
      { uuid: "uuid-1", path: "data" },
      expect.anything(),
    );
  });

  it("toggles read-only and sets the default", async () => {
    const user = userEvent.setup();
    render(<BtrfsSubvolumesCard uuid="uuid-1" />);
    const dataRow = await rowFor("data");

    await user.click(within(dataRow).getByRole("checkbox"));
    expect(route("btrfs_subvolume_set_readonly")).toHaveBeenCalledWith({
      uuid: "uuid-1",
      path: "data",
      readOnly: true,
    });

    await user.click(
      within(dataRow).getByRole("button", { name: "Set default" }),
    );
    expect(route("btrfs_subvolume_set_default")).toHaveBeenCalledWith({
      uuid: "uuid-1",
      path: "data",
    });
  });

  it("offers Snapshot only when a handler is provided", async () => {
    const user = userEvent.setup();
    const onSnapshot = vi.fn();
    const { unmount } = render(<BtrfsSubvolumesCard uuid="uuid-1" />);
    expect(
      within(await rowFor("data")).queryByRole("button", { name: "Snapshot" }),
    ).not.toBeInTheDocument();
    unmount();

    render(<BtrfsSubvolumesCard onSnapshot={onSnapshot} uuid="uuid-1" />);
    await user.click(
      within(await rowFor("data")).getByRole("button", { name: "Snapshot" }),
    );
    expect(onSnapshot).toHaveBeenCalledWith(data);
  });

  it("sets a size limit when quotas report sizes", async () => {
    const user = userEvent.setup();
    mocks.subvolumes.mockResolvedValue([
      { ...data, referencedBytes: 1024 ** 3, maxReferencedBytes: 1024 ** 3 },
    ]);
    render(<BtrfsSubvolumesCard uuid="uuid-1" />);
    const dataRow = await rowFor("data");
    expect(within(dataRow).getByText("Limit 1 GB")).toBeInTheDocument();

    await user.click(within(dataRow).getByRole("button", { name: "Limit" }));
    const dialog = await screen.findByRole("dialog");
    const field = within(dialog).getByRole("spinbutton", {
      name: "Max referenced (GiB)",
    });
    expect(field).toHaveValue(1);
    await user.clear(field);
    await user.type(field, "2");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(route("btrfs_qgroup_limit")).toHaveBeenCalledWith(
      { uuid: "uuid-1", path: "data", maxReferencedBytes: 2147483648 },
      expect.anything(),
    );

    await user.click(within(dataRow).getByRole("button", { name: "Limit" }));
    const reopened = await screen.findByRole("dialog");
    await user.clear(
      within(reopened).getByRole("spinbutton", {
        name: "Max referenced (GiB)",
      }),
    );
    await user.click(within(reopened).getByRole("button", { name: "Save" }));
    expect(route("btrfs_qgroup_limit")).toHaveBeenLastCalledWith(
      { uuid: "uuid-1", path: "data" },
      expect.anything(),
    );
  });

  it("prefills the limit to two decimals and rejects a zero-byte limit", async () => {
    const user = userEvent.setup();
    mocks.subvolumes.mockResolvedValue([
      { ...data, referencedBytes: 1, maxReferencedBytes: 1500000000 },
    ]);
    render(<BtrfsSubvolumesCard uuid="uuid-1" />);
    await user.click(
      within(await rowFor("data")).getByRole("button", { name: "Limit" }),
    );
    const dialog = await screen.findByRole("dialog");
    const field = within(dialog).getByRole("spinbutton", {
      name: "Max referenced (GiB)",
    });
    expect(field).toHaveValue(1.4);
    fireEvent.change(field, { target: { value: "0.0000000001" } });
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("hides Limit without quota sizes", async () => {
    render(<BtrfsSubvolumesCard uuid="uuid-1" />);
    expect(
      within(await rowFor("data")).queryByRole("button", { name: "Limit" }),
    ).not.toBeInTheDocument();
  });

  it("sets per-subvolume compression", async () => {
    const user = userEvent.setup();
    render(<BtrfsSubvolumesCard uuid="uuid-1" />);

    await user.click(
      within(await rowFor("data")).getByRole("button", { name: "Compression" }),
    );
    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("combobox", { name: "Compression" }),
    );
    await user.click(screen.getByRole("option", { name: "zstd" }));
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(route("btrfs_subvolume_set_compression")).toHaveBeenCalledWith(
      { uuid: "uuid-1", path: "data", algorithm: "zstd" },
      expect.anything(),
    );
  });
});
