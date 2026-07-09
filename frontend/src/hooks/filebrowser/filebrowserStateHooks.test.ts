import { describe, expect, it } from "vitest";

import { useFileDialogs } from "@/hooks/filebrowser/useFileDialogs";
import { useFileEditor } from "@/hooks/filebrowser/useFileEditor";
import { useFileUpload } from "@/hooks/filebrowser/useFileUpload";
import { act, renderHook } from "@/test/render";

describe("filebrowser state hooks", () => {
  it("tracks dialog state for create, delete, detail, and permissions flows", () => {
    const { result } = renderHook(() => useFileDialogs());
    const initialActions = result.current.actions;

    act(() => {
      result.current.actions.openCreateFile();
      result.current.actions.openCreateFolder();
      result.current.actions.requestDelete(["/tmp/old.txt"]);
      result.current.actions.showDetails(["/tmp/old.txt"]);
      result.current.actions.openPermissions({
        isDirectory: false,
        mode: "0644",
        pathLabel: "old.txt",
        paths: ["/tmp/old.txt"],
        selectionCount: 1,
      });
    });

    expect(result.current.createFileDialog).toBe(true);
    expect(result.current.createFolderDialog).toBe(true);
    expect(result.current.deleteDialog).toBe(true);
    expect(result.current.pendingDeletePaths).toEqual(["/tmp/old.txt"]);
    expect(result.current.detailTarget).toEqual(["/tmp/old.txt"]);
    expect(result.current.permissionsDialog).toMatchObject({
      mode: "0644",
      pathLabel: "old.txt",
    });
    expect(result.current.actions).toBe(initialActions);
  });

  it("closes the delete dialog and clears the pending paths together", () => {
    const { result } = renderHook(() => useFileDialogs());

    act(() => result.current.actions.requestDelete(["/tmp/old.txt"]));
    act(() => result.current.actions.closeDelete());

    expect(result.current.deleteDialog).toBe(false);
    expect(result.current.pendingDeletePaths).toEqual([]);
  });

  it("clears pending delete paths without closing the dialog", () => {
    const { result } = renderHook(() => useFileDialogs());

    act(() => result.current.actions.requestDelete(["/tmp/old.txt"]));
    act(() => result.current.actions.clearPendingDelete());

    expect(result.current.deleteDialog).toBe(true);
    expect(result.current.pendingDeletePaths).toEqual([]);
  });

  it("tracks editor dirty/save/close state and quick-save visibility", () => {
    const { result } = renderHook(() => useFileEditor());
    const initialActions = result.current.actions;

    expect(result.current.showQuickSave).toBe(false);
    expect(result.current.editorRef.current).toBeNull();

    act(() => {
      result.current.actions.openFile("/tmp/note.md");
      result.current.actions.setDirty(true);
      result.current.actions.setSaving(true);
      result.current.actions.promptClose();
    });

    expect(result.current.editingPath).toBe("/tmp/note.md");
    expect(result.current.isEditorDirty).toBe(true);
    expect(result.current.isSavingFile).toBe(true);
    expect(result.current.closeEditorDialog).toBe(true);
    expect(result.current.showQuickSave).toBe(true);
    expect(result.current.actions).toBe(initialActions);
  });

  it("closes the editor in one transition, clearing path, dirt, and prompt", () => {
    const { result } = renderHook(() => useFileEditor());

    act(() => {
      result.current.actions.openFile("/tmp/note.md");
      result.current.actions.setDirty(true);
      result.current.actions.promptClose();
    });
    act(() => result.current.actions.close());

    expect(result.current.editingPath).toBeNull();
    expect(result.current.isEditorDirty).toBe(false);
    expect(result.current.closeEditorDialog).toBe(false);
    expect(result.current.showQuickSave).toBe(false);
  });

  it("dismisses the close prompt without touching the open file", () => {
    const { result } = renderHook(() => useFileEditor());

    act(() => {
      result.current.actions.openFile("/tmp/note.md");
      result.current.actions.setDirty(true);
      result.current.actions.promptClose();
    });
    act(() => result.current.actions.dismissClosePrompt());

    expect(result.current.closeEditorDialog).toBe(false);
    expect(result.current.editingPath).toBe("/tmp/note.md");
    expect(result.current.isEditorDirty).toBe(true);
  });

  it("starts a newly opened file with clean editor state", () => {
    const { result } = renderHook(() => useFileEditor());

    act(() => {
      result.current.actions.openFile("/tmp/first.md");
      result.current.actions.setDirty(true);
      result.current.actions.promptClose();
    });
    act(() => result.current.actions.openFile("/tmp/second.md"));

    expect(result.current.editingPath).toBe("/tmp/second.md");
    expect(result.current.isEditorDirty).toBe(false);
    expect(result.current.closeEditorDialog).toBe(false);
  });

  it("tracks upload state and summarizes file/folder entries", () => {
    const { result } = renderHook(() => useFileUpload());
    const file = new File(["content"], "compose.yaml");

    act(() => {
      result.current.actions.openDialog();
      result.current.actions.setProcessing(true);
      result.current.actions.mergeEntries([
        {
          isDirectory: true,
          relativePath: "stack",
        },
        {
          file,
          isDirectory: false,
          relativePath: "stack/compose.yaml",
        },
      ]);
    });

    expect(result.current.uploadDialogOpen).toBe(true);
    expect(result.current.isUploadProcessing).toBe(true);
    expect(result.current.uploadEntries).toHaveLength(2);
    expect(result.current.uploadSummary).toEqual({
      files: 1,
      folders: 1,
    });
    expect(result.current.fileInputRef.current).toBeNull();
    expect(result.current.folderInputRef.current).toBeNull();
  });

  it("opens the upload dialog with a clean entry list and clears it on close", () => {
    const { result } = renderHook(() => useFileUpload());

    act(() => {
      result.current.actions.mergeEntries([
        { isDirectory: true, relativePath: "stale" },
      ]);
    });
    act(() => result.current.actions.openDialog());

    expect(result.current.uploadDialogOpen).toBe(true);
    expect(result.current.uploadEntries).toEqual([]);

    act(() => {
      result.current.actions.mergeEntries([
        { isDirectory: true, relativePath: "stack" },
      ]);
    });
    act(() => result.current.actions.closeDialog());

    expect(result.current.uploadDialogOpen).toBe(false);
    expect(result.current.uploadEntries).toEqual([]);
  });
});
