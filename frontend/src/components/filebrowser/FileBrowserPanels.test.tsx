import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";

import {
  FileBrowserEditorDialog,
  FileBrowserSaveConflictDialog,
} from "@/components/filebrowser/FileBrowserPanels";
import type { FileEditorHandle } from "@/components/filebrowser/FileEditor";
import { fireEvent, render, screen } from "@/test/render";

vi.mock("@/components/filebrowser/FileBrowserHeader", () => ({
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
