import { describe, expect, it } from "vitest";

import { act, renderHook } from "@/test/render";

import type { FileItem } from "../-types";
import { useFileDialogs } from "./useFileDialogs";
import { useFileUpload } from "./useFileUpload";

describe("filebrowser state hooks", () => {
  it("tracks dialog state for create, delete, detail, and permissions flows", () => {
    const { result } = renderHook(() => useFileDialogs());
    const initialActions = result.current.actions;

    act(() => {
      const item: FileItem = {
        name: "old.txt",
        path: "/tmp/old.txt",
        type: "file",
      };
      result.current.actions.openCreateFile();
      result.current.actions.requestDelete(["/tmp/old.txt"], "trash");
      result.current.actions.showDetails(["/tmp/old.txt"], [item]);
      result.current.actions.openPermissions({
        isDirectory: false,
        mode: "0644",
        pathLabel: "old.txt",
        paths: ["/tmp/old.txt"],
        selectionCount: 1,
      });
    });

    expect(result.current.createFileDialog).toBe(true);
    expect(result.current.deleteDialog).toBe(true);
    expect(result.current.pendingDeletePaths).toEqual(["/tmp/old.txt"]);
    expect(result.current.detailTarget).toEqual(["/tmp/old.txt"]);
    expect(result.current.detailItems).toEqual([
      { name: "old.txt", path: "/tmp/old.txt", type: "file" },
    ]);
    expect(result.current.permissionsDialog).toMatchObject({
      mode: "0644",
      pathLabel: "old.txt",
    });
    expect(result.current.permissionsDialogOpen).toBe(true);
    expect(result.current.actions).toBe(initialActions);
  });

  it("retains permission data until the dialog exit completes", () => {
    const { result } = renderHook(() => useFileDialogs());

    act(() =>
      result.current.actions.openPermissions({
        isDirectory: false,
        mode: "0644",
        pathLabel: "old.txt",
        paths: ["/tmp/old.txt"],
        selectionCount: 1,
      }),
    );
    act(() => result.current.actions.closePermissions());

    expect(result.current.permissionsDialogOpen).toBe(false);
    expect(result.current.permissionsDialog).not.toBeNull();

    act(() => result.current.actions.clearPermissions());
    expect(result.current.permissionsDialog).toBeNull();
  });

  it("closes the delete dialog and clears the pending paths together", () => {
    const { result } = renderHook(() => useFileDialogs());

    act(() => result.current.actions.requestDelete(["/tmp/old.txt"], "trash"));
    act(() => result.current.actions.closeDelete());

    expect(result.current.deleteDialog).toBe(false);
    expect(result.current.pendingDeletePaths).toEqual([]);
  });

  it("remembers the requested delete mode", () => {
    const { result } = renderHook(() => useFileDialogs());

    act(() =>
      result.current.actions.requestDelete(["/tmp/old.txt"], "permanent"),
    );

    expect(result.current.deleteMode).toBe("permanent");
  });

  it("clears pending delete paths without closing the dialog", () => {
    const { result } = renderHook(() => useFileDialogs());

    act(() => result.current.actions.requestDelete(["/tmp/old.txt"], "trash"));
    act(() => result.current.actions.clearPendingDelete());

    expect(result.current.deleteDialog).toBe(true);
    expect(result.current.pendingDeletePaths).toEqual([]);
  });

  it("tracks upload state and summarizes file/folder entries", () => {
    const { result } = renderHook(() => useFileUpload());
    const file = new File(["content"], "compose.yaml");

    act(() => {
      result.current.actions.openDialog();
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
    act(() =>
      result.current.actions.setStatus(
        result.current.uploadEntries,
        "checking",
      ),
    );

    expect(result.current.uploadDialogOpen).toBe(true);
    expect(result.current.uploadStatus).toBe("checking");
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
      result.current.actions.setOwnUploads(true);
    });
    act(() => result.current.actions.openDialog());

    expect(result.current.uploadDialogOpen).toBe(true);
    expect(result.current.uploadEntries).toEqual([]);
    expect(result.current.ownUploads).toBe(false);

    act(() => {
      result.current.actions.mergeEntries([
        { isDirectory: true, relativePath: "stack" },
      ]);
      result.current.actions.setOwnUploads(true);
    });
    expect(result.current.ownUploads).toBe(true);
    act(() => result.current.actions.closeDialog());

    expect(result.current.uploadDialogOpen).toBe(false);
    expect(result.current.uploadEntries).toEqual([]);
    expect(result.current.ownUploads).toBe(false);
  });

  it("keeps an upload's entries while it runs and lets its end touch only its own dialog", () => {
    const { result } = renderHook(() => useFileUpload());
    const file = new File(["content"], "a.txt");

    act(() => result.current.actions.openDialog());
    act(() =>
      result.current.actions.mergeEntries([
        { file, isDirectory: false, relativePath: "a.txt" },
      ]),
    );
    const first = result.current.uploadEntries;
    act(() => result.current.actions.setStatus(first, "uploading"));
    act(() =>
      result.current.actions.mergeEntries([
        { isDirectory: true, relativePath: "late" },
      ]),
    );
    expect(result.current.uploadEntries).toBe(first);

    // Closed while the upload runs, then opened again for another upload.
    act(() => result.current.actions.closeDialog());
    expect(result.current.uploadStatus).toBe("idle");
    act(() => result.current.actions.openDialog());
    act(() =>
      result.current.actions.mergeEntries([
        { isDirectory: true, relativePath: "stack" },
      ]),
    );
    const second = result.current.uploadEntries;
    act(() => result.current.actions.setStatus(second, "uploading"));

    // The first upload's end leaves the dialog and its upload as they are.
    act(() => result.current.actions.closeDialogFor(first));
    act(() => result.current.actions.setStatus(first, "idle"));
    expect(result.current.uploadDialogOpen).toBe(true);
    expect(result.current.uploadEntries).toBe(second);
    expect(result.current.uploadStatus).toBe("uploading");

    // Its own upload's end closes it.
    act(() => result.current.actions.closeDialogFor(second));
    expect(result.current.uploadDialogOpen).toBe(false);
    expect(result.current.uploadEntries).toEqual([]);
    expect(result.current.uploadStatus).toBe("idle");
  });
});
