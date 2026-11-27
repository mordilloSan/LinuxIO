import { createRef, useState } from "react";
import { describe, expect, it, vi } from "vitest";

import type { FileEditorHandle } from "@/components/editor/FileEditor";
import { fireEvent, render, screen } from "@/test/render";

import type { UploadStatus } from "../-hooks/useFileUpload";
import {
  FileBrowserConflictDialog,
  FileBrowserDeleteDialog,
  FileBrowserEditorDialog,
  FileBrowserSaveConflictDialog,
  FileBrowserUploadDialog,
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

describe("file browser upload dialog", () => {
  const ownershipBox = "Make me the owner of new files and folders";
  const renderUpload = (
    privileged: boolean,
    ownUploads = false,
    uploadStatus: UploadStatus = "idle",
  ) => {
    const onClose = vi.fn();
    const onOwnUploadsChange = vi.fn();
    const utils = render(
      <FileBrowserUploadDialog
        fileInputRef={createRef<HTMLInputElement>()}
        folderInputRef={createRef<HTMLInputElement>()}
        normalizedPath="/home/alice"
        onChangeUploadInput={vi.fn()}
        onClearUploadSelection={vi.fn()}
        onClose={onClose}
        onOwnUploadsChange={onOwnUploadsChange}
        onPickFiles={vi.fn()}
        onPickFolder={vi.fn()}
        onStartUpload={vi.fn()}
        open
        ownUploads={ownUploads}
        uploadEntries={[
          {
            file: new File(["x"], "a.txt"),
            isDirectory: false,
            relativePath: "a.txt",
          },
        ]}
        uploadStatus={uploadStatus}
        uploadSummary={{ files: 1, folders: 0 }}
      />,
      { auth: { privileged } },
    );
    return { ...utils, onClose, onOwnUploadsChange };
  };

  it("offers ownership only to a privileged session, unticked", () => {
    renderUpload(true);
    expect(
      screen.getByRole("checkbox", { name: ownershipBox }),
    ).not.toBeChecked();
  });

  it("hides ownership from an unprivileged session", () => {
    renderUpload(false);
    expect(
      screen.queryByRole("checkbox", { name: ownershipBox }),
    ).not.toBeInTheDocument();
  });

  it("reports the ownership choice", () => {
    const { onOwnUploadsChange } = renderUpload(true);
    fireEvent.click(screen.getByRole("checkbox", { name: ownershipBox }));
    expect(onOwnUploadsChange).toHaveBeenCalledWith(true);
  });

  it("holds itself while it checks the destination", async () => {
    const { onClose, user } = renderUpload(true, false, "checking");

    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Uploading..." })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("can be closed once the upload runs and holds everything else", async () => {
    const { onClose, user } = renderUpload(true, false, "uploading");

    // Closing leaves the upload running; the task tray has its Cancel.
    for (const name of [
      "Select files",
      "Select folders",
      "Clear",
      "Uploading...",
    ]) {
      expect(screen.getByRole("button", { name })).toBeDisabled();
    }
    expect(screen.getByRole("checkbox", { name: ownershipBox })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});

describe("file browser delete dialog", () => {
  const onDeleteForGoodInstead = vi.fn();
  const renderDelete = (
    mode: "permanent" | "trash",
    paths: string[],
    knownSize?: number,
  ) =>
    render(
      <FileBrowserDeleteDialog
        knownSize={knownSize}
        mode={mode}
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        onDeleteForGoodInstead={onDeleteForGoodInstead}
        open
        pendingDeletePaths={paths}
      />,
    );

  it("names up to three items", () => {
    renderDelete("trash", ["/srv/report.txt", "/srv/photos/"]);
    expect(
      screen.getByText(/^Move report\.txt and photos to Trash\?/),
    ).toBeInTheDocument();
  });

  it("says what a move to Trash leaves on the disk", () => {
    renderDelete("trash", ["/srv/go-build/"], 36_507_222_016);
    expect(
      screen.getByText(
        "Move go-build to Trash? It stays on the disk (34 GB) until the Trash is emptied.",
      ),
    ).toBeInTheDocument();
  });

  it("says it without a size when the page knows none", () => {
    renderDelete("trash", ["/srv/report.txt", "/srv/photos/"]);
    expect(
      screen.getByText(
        "Move report.txt and photos to Trash? They stay on the disk until the Trash is emptied.",
      ),
    ).toBeInTheDocument();
  });

  it("offers to delete for good instead, from a move to Trash only", async () => {
    onDeleteForGoodInstead.mockClear();
    const { user, unmount } = renderDelete("trash", ["/srv/go-build/"]);
    await user.click(
      screen.getByRole("button", { name: "Delete permanently instead" }),
    );
    expect(onDeleteForGoodInstead).toHaveBeenCalledOnce();
    unmount();

    renderDelete("permanent", ["/srv/go-build/"]);
    expect(
      screen.queryByRole("button", { name: "Delete permanently instead" }),
    ).not.toBeInTheDocument();
  });

  it("asks about the permanent delete in the same dialog, with the focus on Delete", async () => {
    const Asking = () => {
      const [mode, setMode] = useState<"permanent" | "trash">("trash");
      return (
        <FileBrowserDeleteDialog
          mode={mode}
          onClose={vi.fn()}
          onConfirm={vi.fn()}
          onDeleteForGoodInstead={() => setMode("permanent")}
          open
          pendingDeletePaths={["/srv/go-build/"]}
        />
      );
    };
    const { user } = render(<Asking />);

    await user.click(
      screen.getByRole("button", { name: "Delete permanently instead" }),
    );

    expect(
      screen.getByText("Permanently delete go-build? This cannot be undone."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete" })).toHaveFocus();
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
      screen.getByText(/^Move report\.txt, photos and 3 more to Trash\?/),
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
