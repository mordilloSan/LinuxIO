import { describe, expect, it, vi } from "vitest";

import type { DroppedEntry } from "@/hooks/filebrowser/useFileDroppedEntries";
import type { FileResource } from "@/types/filebrowser";

const droppedEntriesMocks = vi.hoisted(() => ({
  extract: vi.fn(),
}));

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("@/hooks/filebrowser/useFileDroppedEntries", () => ({
  useFileDroppedEntries: () => droppedEntriesMocks.extract,
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    warning: toastMocks.warning,
  },
}));

const { useFileDragAndDrop } =
  await import("@/hooks/filebrowser/useFileDragAndDrop");
const { act, renderHook } = await import("@/test/render");

const directory: FileResource = {
  items: [],
  name: "target",
  path: "/srv/target",
  type: "directory",
};

const fileResource: FileResource = {
  name: "note.txt",
  path: "/srv/target/note.txt",
  type: "file",
};

function dragEvent(overrides: Record<string, unknown> = {}) {
  return {
    currentTarget: {
      contains: vi.fn(() => false),
    },
    dataTransfer: {
      dropEffect: "none",
      types: ["Files"],
    },
    preventDefault: vi.fn(),
    relatedTarget: null,
    ...overrides,
  } as any;
}

const droppedFile: DroppedEntry = {
  file: new File(["content"], "compose.yaml"),
  isDirectory: false,
  relativePath: "compose.yaml",
};

describe("useFileDragAndDrop", () => {
  it("marks directory file drops as drag-over and sets copy drop effect", () => {
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete: vi.fn(),
        resource: directory,
        startUpload: vi.fn(),
      }),
    );
    const event = dragEvent();

    act(() => result.current.handleDragEnter(event));
    expect(event.preventDefault).toHaveBeenCalledTimes(1);
    expect(result.current.isDragOver).toBe(true);

    act(() => result.current.handleDragOver(event));
    expect(event.dataTransfer.dropEffect).toBe("copy");
  });

  it("ignores drag events while editing or outside directories", () => {
    const editing = renderHook(() =>
      useFileDragAndDrop({
        editingPath: "/srv/target/note.txt",
        normalizedPath: "/srv/target",
        onUploadComplete: vi.fn(),
        resource: directory,
        startUpload: vi.fn(),
      }),
    );
    const fileTarget = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target/note.txt",
        onUploadComplete: vi.fn(),
        resource: fileResource,
        startUpload: vi.fn(),
      }),
    );
    const editingEvent = dragEvent();
    const fileEvent = dragEvent();

    act(() => editing.result.current.handleDragEnter(editingEvent));
    act(() => fileTarget.result.current.handleDragEnter(fileEvent));

    expect(editingEvent.preventDefault).not.toHaveBeenCalled();
    expect(fileEvent.preventDefault).not.toHaveBeenCalled();
    expect(editing.result.current.isDragOver).toBe(false);
    expect(fileTarget.result.current.isDragOver).toBe(false);
  });

  it("uploads dropped entries and calls completion after successful uploads", async () => {
    droppedEntriesMocks.extract.mockResolvedValue([droppedFile]);
    const startUpload = vi.fn(async () => ({
      conflicts: [],
      failures: [],
      uploaded: 1,
    }));
    const onUploadComplete = vi.fn();
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete,
        resource: directory,
        startUpload,
      }),
    );

    await act(async () => {
      await result.current.handleDrop(dragEvent());
    });

    expect(startUpload).toHaveBeenCalledWith(
      [droppedFile],
      "/srv/target",
      false,
    );
    expect(onUploadComplete).toHaveBeenCalledTimes(1);
    expect(result.current.overwriteTargets).toBeNull();
  });

  it("stores conflicts and can confirm or cancel overwrite", async () => {
    droppedEntriesMocks.extract.mockResolvedValue([droppedFile]);
    const conflict = { ...droppedFile, relativePath: "existing.yaml" };
    const startUpload = vi
      .fn()
      .mockResolvedValueOnce({
        conflicts: [conflict],
        failures: [],
        uploaded: 0,
      })
      .mockResolvedValueOnce({
        conflicts: [],
        failures: [],
        uploaded: 1,
      });
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete: vi.fn(),
        resource: directory,
        startUpload,
      }),
    );

    await act(async () => {
      await result.current.handleDrop(dragEvent());
    });

    expect(result.current.overwriteTargets).toEqual([conflict]);
    expect(toastMocks.warning).toHaveBeenCalledWith(
      "1 item is already present. Overwrite them?",
      expect.anything(),
    );

    await act(async () => {
      await result.current.handleConfirmOverwrite();
    });

    expect(startUpload).toHaveBeenLastCalledWith(
      [conflict],
      "/srv/target",
      true,
    );
    expect(result.current.overwriteTargets).toBeNull();

    act(() => result.current.setOverwriteTargets([conflict]));
    act(() => result.current.handleCancelOverwrite());
    expect(result.current.overwriteTargets).toBeNull();
  });

  it("warns when a drop contains no readable entries", async () => {
    droppedEntriesMocks.extract.mockResolvedValue([]);
    const startUpload = vi.fn();
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete: vi.fn(),
        resource: directory,
        startUpload,
      }),
    );

    await act(async () => {
      await result.current.handleDrop(dragEvent());
    });

    expect(startUpload).not.toHaveBeenCalled();
    expect(toastMocks.warning).toHaveBeenCalledWith(
      "Could not read dropped items. Folder drag-and-drop may not be supported in this browser.",
      expect.anything(),
    );
  });
});
