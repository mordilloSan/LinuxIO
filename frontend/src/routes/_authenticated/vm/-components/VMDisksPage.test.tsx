import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, within } from "@/test/render";

import VMDisksPage from "./VMDisksPage";

const mocks = vi.hoisted(() => ({
  remove: vi.fn(),
  list: vi.fn(),
  disk: {
    name: "linuxio-teste.qcow2",
    path: "/var/lib/libvirt/images/linuxio/cloud-images/linuxio-teste.qcow2",
    vmName: "teste",
    sizeBytes: 2 * 1024 ** 3,
    modifiedAt: "2026-08-24T22:19:19Z",
  },
}));

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: () => ({ mutate: mocks.remove, isPending: false }),
    linuxio: {
      ...actual.linuxio,
      virt: {
        ...actual.linuxio.virt,
        unused_disks: {
          queryKey: ["test-unused-disks"],
          queryFn: () => mocks.list(),
        },
      },
    },
  };
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue([mocks.disk]);
});

describe("VM unused disks", () => {
  it("lists leftover disks with the space they free", async () => {
    render(<VMDisksPage />);
    const table = await screen.findByRole("table", {
      name: "Unused VM disks",
    });
    expect(within(table).getByText("linuxio-teste.qcow2")).toBeInTheDocument();
    expect(within(table).getByText("teste")).toBeInTheDocument();
    expect(screen.getByText(/frees 2 GB/)).toBeInTheDocument();
  });

  it("deletes the chosen disk only after confirmation", async () => {
    const user = userEvent.setup();
    render(<VMDisksPage />);
    await user.click(
      await screen.findByRole("button", {
        name: "Delete linuxio-teste.qcow2",
      }),
    );
    expect(mocks.remove).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(mocks.disk.path)).toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "Delete disk" }),
    );
    expect(mocks.remove).toHaveBeenCalledWith({ path: mocks.disk.path });
  });

  it("says when there is nothing to clean up", async () => {
    mocks.list.mockResolvedValue([]);
    render(<VMDisksPage />);
    expect(await screen.findByText("No unused disks.")).toBeInTheDocument();
  });
});
