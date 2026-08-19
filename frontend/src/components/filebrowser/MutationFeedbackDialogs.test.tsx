import { act } from "react";
import { describe, expect, it, vi } from "vitest";

import FileCard from "@/components/cards/FileCard";
import ConfirmDialog from "@/components/filebrowser/ConfirmDialog";
import FileListRow from "@/components/filebrowser/FileListRow";
import InputDialog from "@/components/filebrowser/InputDialog";
import { fireEvent, render, screen, waitFor } from "@/test/render";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

describe("file mutation feedback dialogs", () => {
  it("keeps a create draft mounted, blocks duplicates, and closes on success", async () => {
    const request = deferred<void>();
    const onClose = vi.fn();
    const onConfirm = vi.fn(() => request.promise);
    const view = render(
      <InputDialog
        label="File Name"
        onClose={onClose}
        onConfirm={onConfirm}
        open
        title="Create File"
      />,
    );

    const input = screen.getByRole("textbox");
    await view.user.type(input, "notes.txt");
    await view.user.click(screen.getByRole("button", { name: "Create" }));

    view.rerender(
      <InputDialog
        isPending
        label="File Name"
        onClose={onClose}
        onConfirm={onConfirm}
        open
        title="Create File"
      />,
    );

    expect(input).toHaveValue("notes.txt");
    expect(input).toBeDisabled();
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("button", { name: "Creating…" })).toBeDisabled();
    await view.user.keyboard("{Enter}{Escape}");
    fireEvent.click(document.querySelector(".app-dialog__backdrop")!);
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();

    await act(async () => request.resolve());
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it("retains the create draft after failure", async () => {
    const request = deferred<void>();
    const onClose = vi.fn();
    const view = render(
      <InputDialog
        label="Folder Name"
        onClose={onClose}
        onConfirm={() => request.promise}
        open
        title="Create Folder"
      />,
    );

    const input = screen.getByRole("textbox");
    await view.user.type(input, "assets");
    await view.user.click(screen.getByRole("button", { name: "Create" }));
    await act(async () => request.reject(new Error("create failed")));

    expect(onClose).not.toHaveBeenCalled();
    expect(input).toHaveValue("assets");
  });

  it("starts with a fresh draft when a create dialog is reopened", async () => {
    const onClose = vi.fn();
    const view = render(
      <InputDialog
        label="File Name"
        onClose={onClose}
        onConfirm={vi.fn()}
        open
        title="Create File"
      />,
    );

    await view.user.type(screen.getByRole("textbox"), "stale.txt");
    view.rerender(
      <InputDialog
        label="File Name"
        onClose={onClose}
        onConfirm={vi.fn()}
        open={false}
        title="Create File"
      />,
    );
    view.rerender(
      <InputDialog
        label="File Name"
        onClose={onClose}
        onConfirm={vi.fn()}
        open
        title="Create File"
      />,
    );

    expect(screen.getByRole("textbox")).toHaveValue("");
  });

  it("shows Task progress and prevents closing a pending delete", async () => {
    const onClose = vi.fn();
    const onConfirm = vi.fn();
    const { user } = render(
      <ConfirmDialog
        isPending
        message="Delete one item?"
        onClose={onClose}
        onConfirm={onConfirm}
        open
        progress={{ percentage: 40, phase: "deleting" }}
        title="Delete Items"
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("deleting (40%)");
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("button", { name: "Deleting…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await user.keyboard("{Enter}{Escape}");
    fireEvent.click(document.querySelector(".app-dialog__backdrop")!);
    expect(onConfirm).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("shows a running item count when delete progress has no percentage", () => {
    render(
      <ConfirmDialog
        isPending
        message="Delete one item?"
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        open
        progress={{ phase: "deleting", detail: { processed: 42 } }}
        title="Delete Items"
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("deleting (42 items)");
  });
});

describe("inline rename feedback", () => {
  it.each([
    ["list", FileListRow],
    ["card", FileCard],
  ] as const)(
    "keeps the %s editor visible and inert while pending",
    (_view, Item) => {
      const onCancelRename = vi.fn();
      const onConfirmRename = vi.fn();
      render(
        <Item
          isDirectory={false}
          isRenaming
          isRenamePending
          name="old.txt"
          onCancelRename={onCancelRename}
          onClick={vi.fn()}
          onConfirmRename={onConfirmRename}
          type="file"
        />,
      );

      const input = screen.getByRole("textbox");
      expect(input).toBeDisabled();
      expect(
        screen.getByRole("progressbar", { name: "Renaming" }),
      ).toBeVisible();
      fireEvent.keyDown(input, { key: "Enter" });
      fireEvent.keyDown(input, { key: "Escape" });
      fireEvent.blur(input);
      expect(onConfirmRename).not.toHaveBeenCalled();
      expect(onCancelRename).not.toHaveBeenCalled();
    },
  );
});
