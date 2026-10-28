import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor, within } from "@/test/render";

import TrashDialog from "./TrashDialog";

const mocks = vi.hoisted(() => ({
  entries: [] as unknown[],
  purge: vi.fn(),
  restore: vi.fn(),
  restoreOptions: {} as { success?: (result: unknown) => void },
  toast: { error: vi.fn(), info: vi.fn(), success: vi.fn(), warning: vi.fn() },
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      filebrowser: {
        trash_list: {
          queryFn: async () => ({ entries: mocks.entries }),
          queryKey: ["linuxio", "filebrowser", "trash_list"],
        },
        trash_purge: {
          useMutation: () => ({
            isPending: false,
            mutate: mocks.purge,
          }),
        },
        trash_restore: {
          useMutation: (options: typeof mocks.restoreOptions) => {
            mocks.restoreOptions = options;
            return { isPending: false, mutate: mocks.restore };
          },
        },
      },
    },
  };
});

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => mocks.toast,
}));

const entries = [
  {
    deletedAt: "2026-01-02T03:04:05Z",
    isDir: false,
    name: "report.pdf",
    originalPath: "/home/alice/docs/report.pdf",
    path: "/home/alice/.local/share/Trash/files/report.pdf",
    size: 2048,
  },
  {
    deletedAt: "",
    isDir: true,
    name: "photos",
    originalPath: "",
    path: "/home/alice/.local/share/Trash/files/photos",
    size: 0,
  },
];

describe("TrashDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.entries = entries;
  });

  it("lists entries with their original folder", async () => {
    render(<TrashDialog onClose={vi.fn()} open />);

    const list = await screen.findByRole("list");
    expect(within(list).getAllByRole("listitem")).toHaveLength(2);
    // The dialog fades in; content can land before the first animation frame.
    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: "Trash (2)" })).toBeVisible(),
    );
    expect(within(list).getByText("report.pdf")).toBeVisible();
    expect(within(list).getByText("/home/alice/docs")).toBeVisible();
    expect(within(list).getByText("Original location unknown")).toBeVisible();
  });

  it("restores the selected entries", async () => {
    const { user } = render(<TrashDialog onClose={vi.fn()} open />);

    await screen.findByRole("list");
    expect(screen.getByRole("button", { name: "Restore" })).toBeDisabled();
    await user.click(
      screen.getByRole("checkbox", { name: "Select report.pdf" }),
    );
    await user.click(screen.getByRole("button", { name: "Restore 1" }));

    expect(mocks.restore).toHaveBeenCalledWith({
      paths: [entries[0].path],
    });
  });

  it("reports per-item failures of a restore", async () => {
    render(<TrashDialog onClose={vi.fn()} open />);
    await screen.findByRole("list");

    mocks.restoreOptions.success?.({
      failed: [{ error: "exists", path: "/x/report.pdf" }],
      succeeded: 0,
      total: 1,
    });

    expect(mocks.toast.error).toHaveBeenCalledWith("Failed to restore 1 item", {
      description: "report.pdf: exists",
    });
  });

  it("confirms before purging everything", async () => {
    const { user } = render(<TrashDialog onClose={vi.fn()} open />);

    await screen.findByRole("list");
    await user.click(screen.getByRole("button", { name: "Empty trash" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Empty trash",
    });
    expect(dialog).toHaveTextContent("cannot be undone");
    expect(mocks.purge).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    expect(mocks.purge).toHaveBeenCalledWith({
      paths: entries.map((entry) => entry.path),
    });
  });

  it("shows the empty state", async () => {
    mocks.entries = [];
    render(<TrashDialog onClose={vi.fn()} open />);

    await waitFor(() =>
      expect(screen.getByText("Trash is empty")).toBeVisible(),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Empty trash" }),
      ).toBeDisabled(),
    );
  });
});
