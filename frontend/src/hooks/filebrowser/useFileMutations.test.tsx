import { beforeEach, describe, expect, it, vi } from "vitest";

import { useFileMutations } from "@/hooks/filebrowser/useFileMutations";
import { act, renderHook } from "@/test/render";

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
}));

function taskEndpoint() {
  return {
    useTaskStreamAction: () => ({
      isPending: false,
      mutateAsync: mocks.mutateAsync,
    }),
  };
}

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      filebrowser: {
        chmod_batch: taskEndpoint(),
        delete_batch: taskEndpoint(),
        resource_patch: taskEndpoint(),
        resource_post: {},
      },
    },
    useCallMutation: () => ({ mutateAsync: vi.fn() }),
  };
});

vi.mock("@/hooks/filebrowser/useListingInvalidation", () => ({
  useListingInvalidation: () => mocks.invalidateListing,
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => mocks.toast,
}));

vi.mock("@/hooks/backgroundTasks/useBackgroundTaskActions", () => ({
  useBackgroundTaskActions: () => ({
    startCompression: mocks.startCompression,
    startCopy: mocks.startCopy,
    startExtraction: mocks.startExtraction,
    startMove: mocks.startMove,
  }),
}));

vi.mock("@/hooks/filebrowser/useFileConflicts", () => ({
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
});
