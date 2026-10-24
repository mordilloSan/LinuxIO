import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";

import type { FileEditorHandle } from "@/components/editor/FileEditor";
import { fireEvent, render, screen } from "@/test/render";

import {
  FileBrowserConflictDialog,
  FileBrowserDeleteDialog,
  FileBrowserEditorDialog,
  FileBrowserSaveConflictDialog,
} from "./FileBrowserPanels";

vi.mock("./FileBrowserHeader", () => ({
  default: () => null,
}));

const existing = { modified: "", size: 0 };

describe("file browser editor dialogs", () => {
  it("shows a recoverable error when file content cannot be loaded", async () => {
    const onCloseEditor = vi.fn();
    render(
      <FileBrowserEditorDialog
        editingFileError={new Error("invalid text")}
        editingPath="/srv/binary"
        editorRef={createRef<FileEditorHandle>()}
        isDirty={false}
        isEditingFileLoading={false}
        isSaving={false}
        onCloseEditor={onCloseEditor}
        onDirtyChange={vi.fn()}
        onSaveContent={vi.fn()}
        onSaveFile={vi.fn()}
        onSearchChange={vi.fn()}
        onSwitchView={vi.fn()}
        onToggleHiddenFiles={vi.fn()}
        searchQuery=""
        showHiddenFiles={false}
        showQuickSave
        viewMode="list"
      />,
    );

    expect(screen.getByText("Unable to open file")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Close editor" }));
    expect(onCloseEditor).toHaveBeenCalledOnce();
  });

  it("renders images inline instead of the text editor", () => {
    render(
      <FileBrowserEditorDialog
        editingFileError={null}
        editingPath="/srv/photos/cat.jpg"
        editorRef={createRef<FileEditorHandle>()}
        isDirty={false}
        isEditingFileLoading={false}
        isSaving={false}
        onCloseEditor={vi.fn()}
        onDirtyChange={vi.fn()}
        onSaveContent={vi.fn()}
        onSaveFile={vi.fn()}
        onSearchChange={vi.fn()}
        onSwitchView={vi.fn()}
        onToggleHiddenFiles={vi.fn()}
        searchQuery=""
        showHiddenFiles={false}
        showQuickSave
        viewMode="list"
      />,
    );

    expect(screen.getByRole("img", { name: "cat.jpg" })).toHaveAttribute(
      "src",
      "/api/download?path=%2Fsrv%2Fphotos%2Fcat.jpg&inline=1",
    );
    expect(screen.queryByText("Unable to open file")).not.toBeInTheDocument();
  });

  it("allows cancelling a save conflict", async () => {
    const onCancel = vi.fn();
    const { user } = render(
      <FileBrowserSaveConflictDialog
        conflict={{ content: "draft" }}
        isSaving={false}
        onCancel={onCancel}
        onOverwrite={vi.fn()}
        onReload={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("labels the folder conflict choice Merge and keeps Overwrite for files", () => {
    render(
      <FileBrowserConflictDialog
        onCancel={vi.fn()}
        onResolve={vi.fn()}
        prompt={{
          destination: "/srv",
          conflicts: [
            { path: "/srv/docs", name: "docs", isDir: true, existing },
            { path: "/srv/a.txt", name: "a.txt", isDir: false, existing },
          ],
        }}
      />,
    );

    expect(screen.getByRole("button", { name: "Merge" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Overwrite" })).toHaveLength(
      1,
    );
    expect(
      screen.getByRole("button", { name: "Overwrite / merge all" }),
    ).toBeInTheDocument();
  });

  it("shows both sides of a conflict and resolves keep both per row and for all", async () => {
    const onResolve = vi.fn();
    const { user } = render(
      <FileBrowserConflictDialog
        onCancel={vi.fn()}
        onResolve={onResolve}
        prompt={{
          destination: "/srv",
          conflicts: [
            {
              path: "/srv/a.txt",
              name: "a.txt",
              isDir: false,
              existing: { modified: "2026-03-12T10:00:00Z", size: 1048576 },
              incoming: { modified: "2026-10-23T10:00:00Z", size: 2097152 },
            },
            {
              path: "/srv/b.txt",
              name: "b.txt",
              isDir: false,
              existing: { modified: "", size: 10 },
              incoming: { size: 20 },
            },
          ],
        }}
      />,
    );

    expect(screen.getByText(/^Existing: 1 MB · .*2026/)).toBeInTheDocument();
    expect(screen.getByText(/^New: 2 MB · .*2026/)).toBeInTheDocument();
    expect(screen.getByText("Existing: 10 Bytes")).toBeInTheDocument();
    expect(screen.getByText("New: 20 Bytes")).toBeInTheDocument();

    await user.click(screen.getAllByRole("button", { name: "Keep both" })[0]);
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(onResolve).toHaveBeenLastCalledWith({
      "/srv/a.txt": "keep",
      "/srv/b.txt": "skip",
    });

    await user.click(screen.getByRole("button", { name: "Keep both for all" }));
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(onResolve).toHaveBeenLastCalledWith({
      "/srv/a.txt": "keep",
      "/srv/b.txt": "keep",
    });
  });
});

describe("file browser delete dialog", () => {
  const renderDelete = (mode: "permanent" | "trash", paths: string[]) =>
    render(
      <FileBrowserDeleteDialog
        mode={mode}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        open
        pendingDeletePaths={paths}
      />,
    );

  it("names up to three items", () => {
    renderDelete("trash", ["/srv/report.txt", "/srv/photos/"]);
    expect(
      screen.getByText("Move report.txt and photos to Trash?"),
    ).toBeInTheDocument();
  });

  it("collapses long selections into 'and N more'", () => {
    renderDelete("trash", [
      "/srv/report.txt",
      "/srv/photos/",
      "/srv/a",
      "/srv/b",
      "/srv/c",
    ]);
    expect(
      screen.getByText("Move report.txt, photos and 3 more to Trash?"),
    ).toBeInTheDocument();
  });

  it("names the item in the permanent delete warning", () => {
    renderDelete("permanent", ["/srv/report.txt"]);
    expect(
      screen.getByText("Permanently delete report.txt? This cannot be undone."),
    ).toBeInTheDocument();
  });

  it("falls back to the count when there are no names", () => {
    renderDelete("permanent", ["/"]);
    expect(
      screen.getByText("Permanently delete 1 item? This cannot be undone."),
    ).toBeInTheDocument();
  });
});
