import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";

import type { FileEditorHandle } from "@/components/editor/FileEditor";
import { fireEvent, render, screen } from "@/test/render";

import {
  FileBrowserEditorDialog,
  FileBrowserSaveConflictDialog,
} from "./FileBrowserPanels";

vi.mock("./FileBrowserHeader", () => ({
  default: () => null,
}));

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
});
