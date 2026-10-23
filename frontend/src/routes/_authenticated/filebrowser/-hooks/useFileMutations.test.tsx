import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, renderHook } from "@/test/render";

import { useFileMutations } from "./useFileMutations";

const mocks = vi.hoisted(() => ({
  mutateAsync: vi.fn().mockResolvedValue(undefined),
  invalidateListing: vi.fn(),
  toast: {
    error: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
  },
  startCompression: vi.fn(),
  startCopy: vi.fn(),
  startExtraction: vi.fn(),
  startMove: vi.fn(),
  resolveCollisions: vi.fn(),
  call: vi.fn(),
  taskOptions: {} as Record<string, { success?: (result: unknown) => void }>,
}));

function taskEndpoint(name?: string) {
  return {
    useTaskStreamAction: (options?: {
      success?: (result: unknown) => void;
    }) => {
      if (name && options) mocks.taskOptions[name] = options;
      return { isPending: false, mutateAsync: mocks.mutateAsync };
    },
  };
}

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    call: mocks.call,
    linuxio: {
      filebrowser: {
        exists_batch: { route: "filebrowser.exists_batch" },
        chmod_batch: taskEndpoint(),
        delete_batch: taskEndpoint("delete"),
        trash_batch: taskEndpoint("trash"),
        resource_patch: taskEndpoint(),
        resource_post: {},
      },
    },
    useCallMutation: () => ({ mutateAsync: vi.fn() }),
  };
});

vi.mock("./useListingInvalidation", () => ({
  useListingInvalidation: () => mocks.invalidateListing,
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => mocks.toast,
}));

vi.mock("@/tasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({
    startCompression: mocks.startCompression,
    startCopy: mocks.startCopy,
    startExtraction: mocks.startExtraction,
    startMove: mocks.startMove,
  }),
}));

vi.mock("./useFileConflicts", () => ({
  CONFLICT_PROMPT_CANCELLED: Symbol("conflict prompt cancelled"),
}));

describe("useFileMutations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("keeps renameItem stable across unrelated rerenders and still renames", async () => {
    const { result, rerender } = renderHook(() =>
      useFileMutations({ normalizedPath: "/srv/projects" }),
    );
    const initialRename = result.current.renameItem;

    // A selection update rerenders the controller, but does not alter the
    // mutation function used by renameItem.
    rerender();
    expect(result.current.renameItem).toBe(initialRename);

    await act(async () => {
      await result.current.renameItem({
        destination: "/srv/projects/renamed.txt",
        from: "/srv/projects/original.txt",
      });
    });

    expect(mocks.mutateAsync).toHaveBeenCalledWith({
      action: "rename",
      dst: "/srv/projects/renamed.txt",
      src: "/srv/projects/original.txt",
    });
  });

  describe("delete and trash feedback", () => {
    const failedResult = {
      failed: [{ error: "permission denied", path: "/srv/projects/a.txt" }],
      succeeded: 1,
      total: 2,
    };

    it("starts the trash task with the selected paths", async () => {
      const { result } = renderHook(() =>
        useFileMutations({ normalizedPath: "/srv/projects" }),
      );

      await act(async () => {
        await result.current.trashItems(["/srv/projects/a.txt"]);
      });

      expect(mocks.mutateAsync).toHaveBeenCalledWith({
        paths: ["/srv/projects/a.txt"],
      });
    });

    it.each(["delete", "trash"])(
      "refreshes the listing and names failed items on a partial %s",
      (name) => {
        const onDeleteSuccess = vi.fn();
        renderHook(() =>
          useFileMutations({
            normalizedPath: "/srv/projects",
            onDeleteSuccess,
          }),
        );

        mocks.taskOptions[name].success?.(failedResult);

        expect(mocks.invalidateListing).toHaveBeenCalledTimes(1);
        expect(onDeleteSuccess).toHaveBeenCalledTimes(1);
        expect(mocks.toast.error).toHaveBeenCalledWith(
          expect.stringContaining("1 item"),
          { description: "a.txt: permission denied" },
        );
        expect(mocks.toast.success).not.toHaveBeenCalled();
      },
    );

    it("toasts success when the trash task has no failures", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/srv/projects" }));

      mocks.taskOptions.trash.success?.({ failed: [], succeeded: 2, total: 2 });

      expect(mocks.invalidateListing).toHaveBeenCalledTimes(1);
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Moved 2 items to Trash",
      );
    });
  });

  describe("keep both on paste", () => {
    beforeEach(() => {
      mocks.call.mockResolvedValue({
        existing: [
          {
            isDir: false,
            modified: "2026-10-23T08:00:00Z",
            path: "/srv/other/b.txt",
            size: 42,
          },
        ],
      });
    });

    it("describes the sources and sends the keep-both sources to the copy task", async () => {
      mocks.resolveCollisions.mockImplementation(async (items: string[]) => ({
        kept: items,
        keepBoth: ["/srv/other/b.txt"],
        overwrite: false,
      }));
      const { result } = renderHook(() =>
        useFileMutations({
          normalizedPath: "/srv/projects",
          resolveCollisions: mocks.resolveCollisions,
        }),
      );

      await act(async () => {
        await result.current.copyItems({
          destinationDir: "/srv/projects",
          sourcePaths: ["/srv/other/b.txt", "/srv/other/c.txt"],
        });
      });

      expect(mocks.call).toHaveBeenCalledWith("filebrowser.exists_batch", {
        paths: ["/srv/other/b.txt", "/srv/other/c.txt"],
      });
      const describeIncoming = mocks.resolveCollisions.mock.calls[0][3] as (
        path: string,
      ) => unknown;
      expect(describeIncoming("/srv/other/b.txt")).toMatchObject({
        modified: "2026-10-23T08:00:00Z",
        size: 42,
      });
      expect(describeIncoming("/srv/other/c.txt")).toBeUndefined();
      expect(mocks.startCopy).toHaveBeenCalledWith(
        expect.objectContaining({
          keepBoth: ["/srv/other/b.txt"],
          overwrite: undefined,
        }),
      );
    });

    it("sends the keep-both sources to the move task and still prompts when the source lookup fails", async () => {
      mocks.call.mockRejectedValue(new Error("offline"));
      mocks.resolveCollisions.mockImplementation(async (items: string[]) => ({
        kept: items,
        keepBoth: [items[0]],
        overwrite: false,
      }));
      const { result } = renderHook(() =>
        useFileMutations({
          normalizedPath: "/srv/projects",
          resolveCollisions: mocks.resolveCollisions,
        }),
      );

      await act(async () => {
        await result.current.moveItems({
          destinationDir: "/srv/projects",
          sourcePaths: ["/srv/other/b.txt"],
        });
      });

      const describeIncoming = mocks.resolveCollisions.mock.calls[0][3] as (
        path: string,
      ) => unknown;
      expect(describeIncoming("/srv/other/b.txt")).toBeUndefined();
      expect(mocks.startMove).toHaveBeenCalledWith(
        expect.objectContaining({ keepBoth: ["/srv/other/b.txt"] }),
      );
    });
  });

  describe("paste into the item's own folder", () => {
    const keepAll = async (items: string[]) => ({
      kept: items,
      keepBoth: [],
      overwrite: false,
    });

    beforeEach(() => {
      mocks.call.mockResolvedValue({ existing: [] });
    });

    it("drops same-folder sources of a move and starts nothing when none remain", async () => {
      const { result } = renderHook(() =>
        useFileMutations({
          normalizedPath: "/srv/projects",
          resolveCollisions: mocks.resolveCollisions,
        }),
      );

      await act(async () => {
        await result.current.moveItems({
          destinationDir: "/srv/projects/",
          sourcePaths: ["/srv/projects/a.txt", "/srv/projects/dir/"],
        });
      });

      expect(mocks.toast.info).toHaveBeenCalledWith(
        "Items are already in this folder",
      );
      expect(mocks.resolveCollisions).not.toHaveBeenCalled();
      expect(mocks.startMove).not.toHaveBeenCalled();
    });

    it("moves only the sources from other folders", async () => {
      mocks.resolveCollisions.mockImplementation(keepAll);
      const { result } = renderHook(() =>
        useFileMutations({
          normalizedPath: "/srv/projects",
          resolveCollisions: mocks.resolveCollisions,
        }),
      );

      await act(async () => {
        await result.current.moveItems({
          destinationDir: "/srv/projects",
          sourcePaths: ["/srv/projects/a.txt", "/srv/other/b.txt"],
        });
      });

      expect(mocks.startMove).toHaveBeenCalledWith(
        expect.objectContaining({ sources: ["/srv/other/b.txt"] }),
      );
    });

    it("keeps same-folder sources in a copy but skips their conflict check", async () => {
      mocks.resolveCollisions.mockImplementation(keepAll);
      const { result } = renderHook(() =>
        useFileMutations({
          normalizedPath: "/srv/projects",
          resolveCollisions: mocks.resolveCollisions,
        }),
      );

      await act(async () => {
        await result.current.copyItems({
          destinationDir: "/srv/projects",
          sourcePaths: ["/srv/projects/a.txt", "/srv/other/b.txt"],
        });
      });

      expect(mocks.resolveCollisions).toHaveBeenCalledWith(
        ["/srv/other/b.txt"],
        expect.any(Function),
        "/srv/projects",
        expect.any(Function),
      );
      expect(mocks.startCopy).toHaveBeenCalledWith(
        expect.objectContaining({
          sources: ["/srv/projects/a.txt", "/srv/other/b.txt"],
        }),
      );
    });
  });
});
