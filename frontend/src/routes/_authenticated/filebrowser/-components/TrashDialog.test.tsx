import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError, TASK_CANCELED_CODE } from "@/api";
import { markTerminalFeedbackEmitted } from "@/tasks/terminalTaskFeedback";
import { render, screen, waitFor, within } from "@/test/render";

import TrashDialog from "./TrashDialog";

type MutationOptions = {
  error?: (error: unknown) => void;
  onStart?: (task: { id: string }) => void;
  success?: (result: unknown, request: { paths: string[] }) => void;
};

const mocks = vi.hoisted(() => ({
  entries: [] as unknown[],
  navigate: vi.fn(),
  purge: vi.fn(),
  purgeOptions: {} as MutationOptions,
  purgePending: false,
  restore: vi.fn(),
  restoreOptions: {} as MutationOptions,
  restorePending: false,
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
          useMutation: (options: MutationOptions) => {
            mocks.purgeOptions = options;
            return { isPending: mocks.purgePending, mutate: mocks.purge };
          },
        },
        trash_restore: {
          useMutation: (options: MutationOptions) => {
            mocks.restoreOptions = options;
            return { isPending: mocks.restorePending, mutate: mocks.restore };
          },
        },
      },
    },
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => mocks.navigate,
}));

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
    mocks.purgePending = false;
    mocks.restorePending = false;
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

  it("names the destination before restoring an unrecorded item into a system folder", async () => {
    const foreign = {
      confirmRestore: true,
      deletedAt: "",
      isDir: false,
      name: "app.conf",
      originalPath: "/etc/app.d/app.conf",
      path: "/home/alice/.local/share/Trash/files/app.conf",
      size: 12,
    };
    mocks.entries = [...entries, foreign];
    const { user } = render(<TrashDialog onClose={vi.fn()} open />);

    await screen.findByRole("list");
    await user.click(screen.getByRole("checkbox", { name: "Select app.conf" }));
    await user.click(screen.getByRole("button", { name: "Restore 1" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Restore to a system folder",
    });
    expect(dialog).toHaveTextContent("Restore to /etc/app.d/app.conf?");
    expect(mocks.restore).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole("button", { name: "Restore" }));
    expect(mocks.restore).toHaveBeenCalledWith({
      confirmedDestinations: { [foreign.path]: foreign.originalPath },
      paths: [foreign.path],
    });
  });

  it("reports per-item failures of a restore", async () => {
    render(<TrashDialog onClose={vi.fn()} open />);
    await screen.findByRole("list");

    mocks.restoreOptions.success?.(
      {
        failed: [{ error: "exists", path: "/x/report.pdf" }],
        succeeded: 0,
        total: 1,
      },
      { paths: [entries[0].path] },
    );

    expect(mocks.toast.error).toHaveBeenCalledWith("Failed to restore 1 item", {
      description: "report.pdf: exists",
    });
  });

  it("offers the real folder when a restore is refused for a link", async () => {
    render(<TrashDialog onClose={vi.fn()} open />);
    await screen.findByRole("list");
    const refusal =
      "forbidden: /home/alice/docs is a link to /mnt/data/docs. Open that folder to make this change";

    mocks.restoreOptions.success?.(
      {
        failed: [{ error: refusal, path: "/x/report.pdf" }],
        succeeded: 0,
        total: 1,
      },
      { paths: [entries[0].path] },
    );

    expect(mocks.toast.error).toHaveBeenCalledWith("Failed to restore 1 item", {
      action: expect.objectContaining({ label: "Open the real folder" }),
      description: `report.pdf: ${refusal}`,
      duration: 10_000,
    });
    const { action } = mocks.toast.error.mock.calls[0][1] as {
      action: { onClick: (event: unknown) => void };
    };
    action.onClick({});
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/filebrowser/$",
      params: { _splat: "mnt/data/docs" },
      search: {},
    });
  });

  it("closes the dialog when it opens the real folder", async () => {
    const onClose = vi.fn();
    render(<TrashDialog onClose={onClose} open />);
    await screen.findByRole("list");
    const refusal =
      "forbidden: /home/alice/docs is a link to /mnt/data/docs. Open that folder to make this change";

    mocks.restoreOptions.success?.(
      {
        failed: [{ error: refusal, path: "/x/report.pdf" }],
        succeeded: 0,
        total: 1,
      },
      { paths: [entries[0].path] },
    );
    expect(onClose).not.toHaveBeenCalled();
    const { action } = mocks.toast.error.mock.calls[0][1] as {
      action: { onClick: (event: unknown) => void };
    };
    action.onClick({});

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(mocks.navigate).toHaveBeenCalledTimes(1);
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

  it("closes the Empty trash confirm while the purge runs and holds the actions", async () => {
    const view = render(<TrashDialog onClose={vi.fn()} open />);

    await screen.findByRole("list");
    await view.user.click(screen.getByRole("button", { name: "Empty trash" }));
    const confirm = await screen.findByRole("dialog", { name: "Empty trash" });
    await view.user.click(
      within(confirm).getByRole("button", { name: "Delete" }),
    );
    mocks.purgePending = true;
    view.rerender(<TrashDialog onClose={vi.fn()} open />);

    expect(
      within(confirm).getByRole("button", { name: "Deleting…" }),
    ).toBeDisabled();
    await view.user.click(
      within(confirm).getByRole("button", { name: "Close" }),
    );

    const trash = await screen.findByRole("dialog", { name: "Trash (2)" });
    expect(within(trash).getByRole("status")).toHaveTextContent(
      "Deleting items",
    );
    for (const name of ["Restore", "Delete permanently", "Empty trash"]) {
      expect(within(trash).getByRole("button", { name })).toBeDisabled();
    }
    expect(mocks.purge).toHaveBeenCalledTimes(1);
  });

  it("closes while a job runs and reports its result once", async () => {
    const onClose = vi.fn();
    mocks.purgePending = true;
    const { user } = render(<TrashDialog onClose={onClose} open />);

    await screen.findByRole("list");
    // The dialog reports the job, so the task tray does not as well.
    mocks.purgeOptions.onStart?.({ id: "purge-job" });
    expect(markTerminalFeedbackEmitted("purge-job")).toBe(false);
    await user.click(screen.getByRole("button", { name: "Close" }));
    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(2);

    // The job runs on and its result is reported by the mutation, not by
    // the dialog that was closed.
    act(() =>
      mocks.purgeOptions.success?.(
        { failed: [], succeeded: 2, total: 2 },
        { paths: entries.map((entry) => entry.path) },
      ),
    );
    expect(mocks.toast.success).toHaveBeenCalledTimes(1);
    expect(mocks.toast.success).toHaveBeenCalledWith("Deleted 2 items");
    expect(mocks.purge).not.toHaveBeenCalled();
  });

  it("does not report a cancelled job as a failure", async () => {
    render(<TrashDialog onClose={vi.fn()} open />);
    await screen.findByRole("list");

    act(() =>
      mocks.restoreOptions.error?.(
        new LinuxIOError("operation aborted", TASK_CANCELED_CODE),
      ),
    );
    expect(mocks.toast.error).not.toHaveBeenCalled();
    act(() => mocks.restoreOptions.error?.(new LinuxIOError("disk full", 500)));
    expect(mocks.toast.error).toHaveBeenCalledTimes(1);
  });

  it("keeps what was selected in a dialog opened again when the job ends", async () => {
    const onClose = vi.fn();
    mocks.restorePending = true;
    const view = render(<TrashDialog onClose={onClose} open />);

    await screen.findByRole("list");
    await view.user.click(screen.getByRole("button", { name: "Close" }));
    view.rerender(<TrashDialog onClose={onClose} open={false} />);
    view.rerender(<TrashDialog onClose={onClose} open />);
    await view.user.click(
      await screen.findByRole("checkbox", { name: "Select photos" }),
    );

    act(() =>
      mocks.restoreOptions.success?.(
        { failed: [], succeeded: 1, total: 1 },
        { paths: [entries[0].path] },
      ),
    );

    expect(
      screen.getByRole("checkbox", { name: "Select photos" }),
    ).toBeChecked();
    expect(mocks.toast.success).toHaveBeenCalledTimes(1);
    expect(mocks.toast.success).toHaveBeenCalledWith("Restored 1 item");
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
