import { describe, expect, it, vi } from "vitest";

import type { ResolveCollisionsFn } from "@/hooks/filebrowser/useFileConflicts";
import type { DroppedEntry } from "@/hooks/filebrowser/useFileDroppedEntries";
import type { FileResource } from "@/types/filebrowser";

const droppedEntriesMocks = vi.hoisted(() => ({
  extract: vi.fn(),
}));

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("@/hooks/filebrowser/useFileDroppedEntries", () => ({
  useFileDroppedEntries: () => droppedEntriesMocks.extract,
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastMocks.error,
    info: toastMocks.info,
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

// Pass-through resolution: no collisions found, keep everything.
const passthroughCollisions = () =>
  vi.fn(async (items: unknown[]) => ({
    kept: items,
    overwrite: false,
  })) as unknown as ResolveCollisionsFn;

describe("useFileDragAndDrop", () => {
  it("marks directory file drops as drag-over and sets copy drop effect", () => {
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete: vi.fn(),
        resolveCollisions: passthroughCollisions(),
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
        resolveCollisions: passthroughCollisions(),
        resource: directory,
        startUpload: vi.fn(),
      }),
    );
    const fileTarget = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target/note.txt",
        onUploadComplete: vi.fn(),
        resolveCollisions: passthroughCollisions(),
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
    const resolveCollisions = passthroughCollisions();
    const startUpload = vi.fn(async () => ({
      failures: [],
      uploaded: 1,
    }));
    const onUploadComplete = vi.fn();
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete,
        resolveCollisions,
        resource: directory,
        startUpload,
      }),
    );

    await act(async () => {
      await result.current.handleDrop(dragEvent());
    });

    expect(resolveCollisions).toHaveBeenCalledWith(
      [droppedFile],
      expect.any(Function),
      "/srv/target",
    );
    expect(startUpload).toHaveBeenCalledWith(
      [droppedFile],
      "/srv/target",
      false,
    );
    expect(onUploadComplete).toHaveBeenCalledTimes(1);
  });

  it("does not upload when the conflict prompt is cancelled", async () => {
    droppedEntriesMocks.extract.mockResolvedValue([droppedFile]);
    const resolveCollisions = vi
      .fn()
      .mockResolvedValue(null) as unknown as ResolveCollisionsFn;
    const startUpload = vi.fn();
    const onUploadComplete = vi.fn();
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete,
        resolveCollisions,
        resource: directory,
        startUpload,
      }),
    );

    await act(async () => {
      await result.current.handleDrop(dragEvent());
    });

    expect(startUpload).not.toHaveBeenCalled();
    expect(onUploadComplete).not.toHaveBeenCalled();
  });

  it("uploads only kept items with overwrite when the user resolves conflicts", async () => {
    const second: DroppedEntry = {
      file: new File(["other"], "notes.md"),
      isDirectory: false,
      relativePath: "notes.md",
    };
    droppedEntriesMocks.extract.mockResolvedValue([droppedFile, second]);
    const resolveCollisions = vi.fn().mockResolvedValue({
      kept: [second],
      overwrite: true,
    }) as unknown as ResolveCollisionsFn;
    const startUpload = vi.fn(async () => ({
      failures: [],
      uploaded: 1,
    }));
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete: vi.fn(),
        resolveCollisions,
        resource: directory,
        startUpload,
      }),
    );

    await act(async () => {
      await result.current.handleDrop(dragEvent());
    });

    expect(startUpload).toHaveBeenCalledWith([second], "/srv/target", true);
  });

  it("does not start a task when every dropped item is skipped", async () => {
    droppedEntriesMocks.extract.mockResolvedValue([droppedFile]);
    const resolveCollisions = vi.fn().mockResolvedValue({
      kept: [],
      overwrite: false,
    }) as unknown as ResolveCollisionsFn;
    const startUpload = vi.fn();
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete: vi.fn(),
        resolveCollisions,
        resource: directory,
        startUpload,
      }),
    );

    await act(async () => {
      await result.current.handleDrop(dragEvent());
    });

    expect(startUpload).not.toHaveBeenCalled();
    expect(toastMocks.info).toHaveBeenCalledWith(
      "All items skipped",
      expect.anything(),
    );
  });

  it("warns when a drop contains no readable entries", async () => {
    droppedEntriesMocks.extract.mockResolvedValue([]);
    const startUpload = vi.fn();
    const { result } = renderHook(() =>
      useFileDragAndDrop({
        normalizedPath: "/srv/target",
        onUploadComplete: vi.fn(),
        resolveCollisions: passthroughCollisions(),
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
