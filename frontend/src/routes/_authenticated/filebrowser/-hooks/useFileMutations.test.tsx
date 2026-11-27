import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { LinuxIOError, TASK_CANCELED_CODE } from "@/api";
import { markTerminalFeedbackEmitted } from "@/tasks/terminalTaskFeedback";
import {
  act,
  createTestQueryClient,
  renderHook as renderBareHook,
} from "@/test/render";

import { nextNoTrashPrompt, settleNoTrashPrompt } from "./noTrashPrompts";
import { useFileMutations } from "./useFileMutations";

const mocks = vi.hoisted(() => ({
  mutateAsync: vi.fn().mockResolvedValue(undefined),
  invalidateListing: vi.fn(),
  navigate: vi.fn(),
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
  router: { state: { location: { pathname: "/filebrowser/srv/projects" } } },
  taskOptions: {} as Record<
    string,
    {
      error?: (error: Error, request?: unknown) => void;
      onStart?: (task: { id: string }) => void;
      success?: (result: unknown, request?: { paths: string[] }) => void;
    }
  >,
}));

function jobEndpoint(name?: string) {
  return {
    useMutation: (options?: {
      error?: (error: Error, request?: unknown) => void;
      onStart?: (task: { id: string }) => void;
      success?: (result: unknown, request?: { paths: string[] }) => void;
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
        // What a move to Trash reads its items' sizes from.
        list_directory: actual.linuxio.filebrowser.list_directory,
        subfolders: actual.linuxio.filebrowser.subfolders,
        exists_batch: { route: "filebrowser.exists_batch" },
        chmod_batch: jobEndpoint("chmod"),
        delete_batch: jobEndpoint("delete"),
        trash_batch: jobEndpoint("trash"),
        resource_patch: jobEndpoint(),
        resource_post: {
          useMutation: () => ({ isPending: false, mutateAsync: vi.fn() }),
        },
      },
    },
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => mocks.navigate,
  useRouter: () => mocks.router,
}));

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

// What the page holds of the folders it has shown, which a move to Trash
// reads its items' sizes from.
let queryClient: QueryClient;
const renderHook: typeof renderBareHook = (callback, options) =>
  renderBareHook(callback, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
    ...options,
  });

describe("useFileMutations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    queryClient = createTestQueryClient();
    mocks.router.state.location.pathname = "/filebrowser/srv/projects";
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

        const paths = ["/srv/projects/a.txt", "/srv/projects/b.txt"];
        mocks.taskOptions[name].success?.(failedResult, { paths });

        expect(mocks.invalidateListing).toHaveBeenCalledTimes(1);
        // Only the job's own items leave the selection.
        expect(onDeleteSuccess).toHaveBeenCalledTimes(1);
        expect(onDeleteSuccess).toHaveBeenCalledWith(paths);
        expect(mocks.toast.error).toHaveBeenCalledWith(
          expect.stringContaining("1 item"),
          { description: "a.txt: permission denied" },
        );
        expect(mocks.toast.success).not.toHaveBeenCalled();
      },
    );

    it("names failed items when a permissions change partly fails", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/srv/projects" }));

      mocks.taskOptions.chmod.success?.(failedResult);

      expect(mocks.toast.error).toHaveBeenCalledWith(
        "Failed to change permissions on 1 of 2 items",
        { description: "a.txt: permission denied" },
      );
    });

    describe("Try again after a partial permission change", () => {
      const chmodRequest = {
        group: "staff",
        mode: "750",
        owner: "alice",
        paths: [
          "/srv/projects/a.txt",
          "/srv/projects/docs/",
          "/srv/projects/b.txt",
        ],
        recursive: true,
      };
      const partlyFailed = {
        failed: [
          { error: "permission denied", path: "/srv/projects/a.txt" },
          { error: "permission denied", path: "/srv/projects/docs/" },
        ],
        succeeded: 1,
        total: 3,
      };

      it("opens the dialog again for the failed items with the same settings", () => {
        const onPermissionsRetry = vi.fn();
        renderHook(() =>
          useFileMutations({
            normalizedPath: "/srv/projects",
            onPermissionsRetry,
          }),
        );

        mocks.taskOptions.chmod.success?.(partlyFailed, chmodRequest);

        expect(mocks.toast.error).toHaveBeenCalledWith(
          "Failed to change permissions on 2 of 3 items",
          {
            action: expect.objectContaining({ label: "Try again" }),
            description: "a.txt: permission denied\ndocs: permission denied",
            duration: 10_000,
          },
        );
        expect(onPermissionsRetry).not.toHaveBeenCalled();
        const { action } = mocks.toast.error.mock.calls[0][1] as {
          action: { onClick: (event: unknown) => void };
        };
        action.onClick({});

        expect(onPermissionsRetry).toHaveBeenCalledWith({
          group: "staff",
          isDirectory: true,
          mode: "750",
          owner: "alice",
          pathLabel: "2 items",
          paths: ["/srv/projects/a.txt", "/srv/projects/docs/"],
          recursive: true,
          selectionCount: 2,
        });
      });

      it("names the one failed item and keeps a change that was not recursive", () => {
        const onPermissionsRetry = vi.fn();
        renderHook(() =>
          useFileMutations({
            normalizedPath: "/srv/projects",
            onPermissionsRetry,
          }),
        );
        const request = {
          group: "",
          mode: "644",
          owner: "",
          paths: ["/srv/projects/a.txt", "/srv/projects/b.txt"],
        };

        mocks.taskOptions.chmod.success?.(failedResult, request);
        const { action } = mocks.toast.error.mock.calls[0][1] as {
          action: { onClick: (event: unknown) => void };
        };
        action.onClick({});

        expect(onPermissionsRetry).toHaveBeenCalledWith({
          group: undefined,
          isDirectory: false,
          mode: "644",
          owner: undefined,
          pathLabel: "/srv/projects/a.txt",
          paths: ["/srv/projects/a.txt"],
          recursive: false,
          selectionCount: 1,
        });
      });

      it("is not offered away from the file browser, where the dialog is not", () => {
        mocks.router.state.location.pathname = "/dashboard";
        const onPermissionsRetry = vi.fn();
        renderHook(() =>
          useFileMutations({
            normalizedPath: "/srv/projects",
            onPermissionsRetry,
          }),
        );

        mocks.taskOptions.chmod.success?.(partlyFailed, chmodRequest);

        expect(mocks.toast.error).toHaveBeenCalledWith(
          "Failed to change permissions on 2 of 3 items",
          {
            description: "a.txt: permission denied\ndocs: permission denied",
          },
        );
      });
    });

    it("offers the real folder when a failed item is a refused link", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/srv/projects" }));
      const refusal =
        "forbidden: /srv/projects/docs is a link to /mnt/data/docs. Open that folder to make this change";

      mocks.taskOptions.delete.success?.(
        {
          failed: [
            { error: "permission denied", path: "/srv/projects/a.txt" },
            { error: refusal, path: "/srv/projects/docs" },
          ],
          succeeded: 0,
          total: 2,
        },
        { paths: ["/srv/projects/a.txt", "/srv/projects/docs"] },
      );

      expect(mocks.toast.error).toHaveBeenCalledWith(
        "Failed to delete 2 items",
        {
          action: expect.objectContaining({ label: "Open the real folder" }),
          description: `a.txt: permission denied\ndocs: ${refusal}`,
          duration: 10_000,
        },
      );
      const { action } = mocks.toast.error.mock.calls[0][1] as {
        action: { onClick: (event: unknown) => void };
      };
      action.onClick({});
      expect(mocks.navigate).toHaveBeenCalledWith({
        to: "/filebrowser/$",
        params: { _splat: "mnt/data/docs" },
        search: {},
      });
    });

    it("closes the permissions dialog when it opens the real folder", () => {
      const onPermissionsLinkOpen = vi.fn();
      renderHook(() =>
        useFileMutations({
          normalizedPath: "/srv/projects",
          onPermissionsLinkOpen,
        }),
      );
      const refusal =
        "forbidden: /srv/projects/docs is a link to /mnt/data/docs. Open that folder to make this change";

      mocks.taskOptions.chmod.success?.({
        failed: [{ error: refusal, path: "/srv/projects/docs" }],
        succeeded: 0,
        total: 1,
      });
      expect(onPermissionsLinkOpen).not.toHaveBeenCalled();
      const { action } = mocks.toast.error.mock.calls[0][1] as {
        action: { onClick: (event: unknown) => void };
      };
      action.onClick({});

      expect(onPermissionsLinkOpen).toHaveBeenCalledTimes(1);
      expect(mocks.navigate).toHaveBeenCalledTimes(1);
    });

    it("offers the real folder when the whole task is refused for a link", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/srv/projects" }));
      const refusal =
        "forbidden: /srv/projects/docs is a link to /mnt/data/docs. Open that folder to make this change";

      // A job's error comes with its request, as its result does.
      mocks.taskOptions.trash.error?.(new Error(refusal), {
        paths: ["/srv/projects/docs/a.txt"],
      });
      mocks.taskOptions.chmod.error?.(new Error("permission denied"));

      expect(mocks.toast.error.mock.calls).toEqual([
        [
          refusal,
          expect.objectContaining({
            action: expect.objectContaining({ label: "Open the real folder" }),
          }),
        ],
        ["permission denied"],
      ]);
    });

    it.each(["delete", "trash", "chmod"])(
      "reports a %s job itself, and not a cancel",
      (name) => {
        const onDeleteSuccess = vi.fn();
        renderHook(() =>
          useFileMutations({
            normalizedPath: "/srv/projects",
            onDeleteSuccess,
          }),
        );
        const options = mocks.taskOptions[name];
        const request = { paths: ["/srv/projects/a.txt"] };

        // Its dialog may be closed: the hook reports the job, so the task
        // tray, which lists it, does not report it a second time.
        options.onStart?.({ id: `${name}-job` });
        expect(markTerminalFeedbackEmitted(`${name}-job`)).toBe(false);

        // A cancel is not a failure; the tray that cancelled it says so.
        act(() =>
          options.error?.(
            new LinuxIOError("operation aborted", TASK_CANCELED_CODE),
            request,
          ),
        );
        expect(mocks.toast.error).not.toHaveBeenCalled();
        // A cancelled delete has removed some of its items already, so none
        // of them stays selected.
        expect(onDeleteSuccess.mock.calls).toEqual(
          name === "chmod" ? [] : [[request.paths]],
        );
        expect(mocks.invalidateListing).toHaveBeenCalledTimes(
          name === "chmod" ? 0 : 1,
        );

        act(() => options.error?.(new LinuxIOError("disk full", 500), request));
        expect(mocks.toast.error).toHaveBeenCalledTimes(1);
        expect(onDeleteSuccess).toHaveBeenCalledTimes(name === "chmod" ? 0 : 1);
      },
    );

    it("toasts success when the trash task has no failures", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/srv/projects" }));

      mocks.taskOptions.trash.success?.(
        { failed: [], succeeded: 2, total: 2 },
        { paths: ["/srv/projects/a.txt", "/srv/projects/b.txt"] },
      );

      expect(mocks.invalidateListing).toHaveBeenCalledTimes(1);
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Moved 2 items to Trash",
        { action: expect.objectContaining({ label: "Open Trash" }) },
      );
    });

    it("says what the moved items still take, and opens the Trash from the toast", async () => {
      const paths = ["/srv/projects/go-build/", "/srv/projects/notes.txt"];
      queryClient.setQueryData(
        ["linuxio", "filebrowser", "subfolders", { path: "/srv/projects" }],
        {
          pending: false,
          subfolders: [
            { path: "/srv/projects/go-build", size: 36_507_222_016 },
          ],
        },
      );
      queryClient.setQueryData(
        ["linuxio", "filebrowser", "list_directory", { path: "/srv/projects" }],
        { files: [{ name: "notes.txt", size: 10 }], folders: [] },
      );
      const { result } = renderHook(() =>
        useFileMutations({ normalizedPath: "/srv/projects" }),
      );

      await act(() => result.current.trashItems(paths));
      mocks.taskOptions.trash.success?.(
        { failed: [], succeeded: 2, total: 2 },
        { paths },
      );

      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Moved 2 items to Trash, 34 GB still used",
        { action: expect.objectContaining({ label: "Open Trash" }) },
      );
      mocks.toast.success.mock.calls[0][1].action.onClick();
      expect(mocks.navigate).toHaveBeenCalledWith({
        params: { _splat: "srv/projects" },
        search: { trash: true },
        to: "/filebrowser/$",
      });
    });

    it("says no size the page did not know when this move started", async () => {
      const paths = ["/srv/projects/build/"];
      const sized = (size: number, pending: boolean) =>
        queryClient.setQueryData(
          ["linuxio", "filebrowser", "subfolders", { path: "/srv/projects" }],
          { pending, subfolders: [{ path: "/srv/projects/build", size }] },
        );
      const { result } = renderHook(() =>
        useFileMutations({ normalizedPath: "/srv/projects" }),
      );

      // A first move of the folder, whose end is never reported here.
      sized(36_507_222_016, false);
      await act(() => result.current.trashItems(paths));
      // The folder is made again and trashed before the index has its size.
      sized(0, true);
      await act(() => result.current.trashItems(paths));
      mocks.taskOptions.trash.success?.(
        { failed: [], succeeded: 1, total: 1 },
        { paths },
      );

      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Moved 1 item to Trash",
        { action: expect.objectContaining({ label: "Open Trash" }) },
      );
    });

    it("says no size when some of the items stayed where they were", async () => {
      const paths = ["/srv/projects/notes.txt", "/srv/projects/b.txt"];
      queryClient.setQueryData(
        ["linuxio", "filebrowser", "list_directory", { path: "/srv/projects" }],
        {
          files: [
            { name: "notes.txt", size: 10 },
            { name: "b.txt", size: 2048 },
          ],
          folders: [],
        },
      );
      const { result } = renderHook(() =>
        useFileMutations({ normalizedPath: "/srv/projects" }),
      );

      await act(() => result.current.trashItems(paths));
      mocks.taskOptions.trash.success?.(
        {
          failed: [{ error: "no trash", path: paths[1], reason: "no_trash" }],
          succeeded: 1,
          total: 2,
        },
        { paths },
      );

      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Moved 1 item to Trash",
        { action: expect.objectContaining({ label: "Open Trash" }) },
      );
      // The question about the item that stayed is not this test's.
      for (let next = nextNoTrashPrompt(); next; next = nextNoTrashPrompt()) {
        settleNoTrashPrompt(next.id);
      }
    });
  });

  describe("items whose disk cannot hold a trash", () => {
    const A = "/mnt/usb/a.txt";
    const B = "/mnt/usb/b.txt";
    const C = "/mnt/usb/c.txt";

    afterEach(() => {
      for (let next = nextNoTrashPrompt(); next; next = nextNoTrashPrompt()) {
        settleNoTrashPrompt(next.id);
      }
    });

    it("asks once about them instead of listing them as failures", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/mnt/usb" }));

      mocks.taskOptions.trash.success?.(
        {
          failed: [
            { error: "no trash on this disk", path: A, reason: "no_trash" },
            { error: "permission denied", path: B },
            { error: "no trash on this disk", path: C, reason: "no_trash" },
          ],
          succeeded: 1,
          total: 4,
        },
        { paths: [A, B, C, "/mnt/usb/d.txt"] },
      );

      expect(nextNoTrashPrompt()?.paths).toEqual([A, C]);
      // Only the other failure is in the toast.
      expect(mocks.toast.error).toHaveBeenCalledTimes(1);
      expect(mocks.toast.error).toHaveBeenCalledWith(
        "Failed to move 1 item to Trash",
        { description: "b.txt: permission denied" },
      );
    });

    it("reports the items that did move and asks about the rest", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/mnt/usb" }));

      mocks.taskOptions.trash.success?.(
        {
          failed: [{ error: "no trash", path: A, reason: "no_trash" }],
          succeeded: 2,
          total: 3,
        },
        { paths: [A, B, C] },
      );

      expect(nextNoTrashPrompt()?.paths).toEqual([A]);
      expect(mocks.toast.error).not.toHaveBeenCalled();
      expect(mocks.toast.success).toHaveBeenCalledWith(
        "Moved 2 items to Trash",
        { action: expect.objectContaining({ label: "Open Trash" }) },
      );
      // Nothing is deleted without the answer.
      expect(mocks.mutateAsync).not.toHaveBeenCalled();
    });

    it("leaves the question as the only report when nothing moved", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/mnt/usb" }));

      mocks.taskOptions.trash.success?.(
        {
          failed: [{ error: "no trash", path: A, reason: "no_trash" }],
          succeeded: 0,
          total: 1,
        },
        { paths: [A] },
      );

      expect(nextNoTrashPrompt()?.paths).toEqual([A]);
      expect(mocks.toast.error).not.toHaveBeenCalled();
      expect(mocks.toast.success).not.toHaveBeenCalled();
    });

    it("asks about each of two jobs", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/mnt/usb" }));
      const noTrash = (path: string) => ({
        failed: [{ error: "no trash", path, reason: "no_trash" as const }],
        succeeded: 0,
        total: 1,
      });

      mocks.taskOptions.trash.success?.(noTrash(A), { paths: [A] });
      mocks.taskOptions.trash.success?.(noTrash(B), { paths: [B] });

      const first = nextNoTrashPrompt();
      expect(first?.paths).toEqual([A]);
      settleNoTrashPrompt(first?.id ?? 0);
      expect(nextNoTrashPrompt()?.paths).toEqual([B]);
    });

    it("never asks about a permanent delete", () => {
      renderHook(() => useFileMutations({ normalizedPath: "/mnt/usb" }));

      mocks.taskOptions.delete.success?.(
        {
          failed: [{ error: "no trash", path: A, reason: "no_trash" }],
          succeeded: 0,
          total: 1,
        },
        { paths: [A] },
      );

      expect(nextNoTrashPrompt()).toBeNull();
      expect(mocks.toast.error).toHaveBeenCalledWith(
        "Failed to delete 1 item",
        { description: "a.txt: no trash" },
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
