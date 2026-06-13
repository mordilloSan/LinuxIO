import { describe, expect, it } from "vitest";

import { useFileDialogs } from "@/hooks/filebrowser/useFileDialogs";
import { useFileEditor } from "@/hooks/filebrowser/useFileEditor";
import { useFileUpload } from "@/hooks/filebrowser/useFileUpload";
import { act, renderHook } from "@/test/render";

describe("filebrowser state hooks", () => {
  it("tracks dialog state for create, delete, detail, permissions, and rename flows", () => {
    const { result } = renderHook(() => useFileDialogs());

    act(() => {
      result.current.setCreateFileDialog(true);
      result.current.setCreateFolderDialog(true);
      result.current.setDeleteDialog(true);
      result.current.setPendingDeletePaths(["/tmp/old.txt"]);
      result.current.setDetailTarget(["/tmp/old.txt"]);
      result.current.setPermissionsDialog({
        isDirectory: false,
        mode: "0644",
        pathLabel: "old.txt",
        paths: ["/tmp/old.txt"],
        selectionCount: 1,
      });
      result.current.setRenameDialog({
        isDirectory: false,
        name: "old.txt",
        path: "/tmp/old.txt",
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
    expect(result.current.renameDialog).toMatchObject({
      name: "old.txt",
      path: "/tmp/old.txt",
    });
  });

  it("tracks editor dirty/save/close state and quick-save visibility", () => {
    const { result } = renderHook(() => useFileEditor());

    expect(result.current.showQuickSave).toBe(false);
    expect(result.current.editorRef.current).toBeNull();

    act(() => {
      result.current.setEditingPath("/tmp/note.md");
      result.current.setIsEditorDirty(true);
      result.current.setIsSavingFile(true);
      result.current.setCloseEditorDialog(true);
    });

    expect(result.current.editingPath).toBe("/tmp/note.md");
    expect(result.current.isEditorDirty).toBe(true);
    expect(result.current.isSavingFile).toBe(true);
    expect(result.current.closeEditorDialog).toBe(true);
    expect(result.current.showQuickSave).toBe(true);
  });

  it("tracks upload state and summarizes file/folder entries", () => {
    const { result } = renderHook(() => useFileUpload());
    const file = new File(["content"], "compose.yaml");

    act(() => {
      result.current.setUploadDialogOpen(true);
      result.current.setIsUploadProcessing(true);
      result.current.setUploadEntries([
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
});
