import { useNavigate, useRouter } from "@tanstack/react-router";
import { useCallback, useState } from "react";

import {
  actionErrorMessage,
  type ActionSourceDestinationRequest,
  call,
  type FileBatchResult,
  type FileChmodBatchRequest,
  type FileExtractRequest,
  type FileProgress,
  getMutationErrorMessage,
  isTaskCancellationError,
  linuxio,
  type TaskProgress,
} from "@/api";
import { endpointQueryPrefix } from "@/api/query-keys";
import { useScopedToast } from "@/hooks/useScopedToast";
import { batchFailureOpts } from "@/tasks/batchFailures";
import { markTerminalFeedbackEmitted } from "@/tasks/terminalTaskFeedback";
import { useBackgroundTaskActions } from "@/tasks/useBackgroundTaskActions";
import { toastFileError } from "@/utils/linkRefusal";
import {
  baseName,
  isDirectoryPath,
  joinPath,
  parentPath,
  stripTrailingSlash,
} from "@/utils/path";

import { askToDeleteForGood } from "./noTrashPrompts";
import {
  CONFLICT_PROMPT_CANCELLED,
  type ResolveCollisionsFn,
} from "./useFileConflicts";
import type { PermissionsDialogState } from "./useFileDialogs";
import { useListingInvalidation } from "./useListingInvalidation";

export const FILES_TOAST_META = {
  label: "Open files",
  params: { _splat: "" },
  to: "/filebrowser/$",
} as const;

/** Every read of the Trash's list, which a move to Trash changes. */
const TRASH_LIST_KEY = endpointQueryPrefix("filebrowser.trash_list");

interface UseFileMutationsParams {
  normalizedPath: string;
  /**
   * Called with a delete's or a move to Trash's paths when the job starts
   * and again when it ends: they leave the selection. At the start, so that
   * a second Delete does not send items a running job is already removing.
   */
  onDeleteSuccess?: (paths: string[]) => void;
  /** Closes the Permissions dialog when its toast opens a link's real folder. */
  onPermissionsLinkOpen?: () => void;
  /** Opens the Permissions dialog, for "Try again" after a partial failure. */
  onPermissionsRetry?: (dialog: PermissionsDialogState) => void;
  resolveCollisions?: ResolveCollisionsFn;
}

interface CompressPayload {
  archiveName?: string;
  destination?: string;
  paths: string[];
}

type ExtractPayload = FileExtractRequest;

type ChmodPayload = Pick<
  FileChmodBatchRequest,
  "mode" | "paths" | "recursive"
> &
  Partial<Pick<FileChmodBatchRequest, "group" | "owner">>;

interface CopyMovePayload {
  destinationDir: string;
  sourcePaths: string[];
}

interface RenamePayload {
  destination: string;
  from: string;
}

export const useFileMutations = ({
  normalizedPath,
  onDeleteSuccess,
  onPermissionsLinkOpen,
  onPermissionsRetry,
  resolveCollisions,
}: UseFileMutationsParams) => {
  const [createPending, setCreatePending] = useState<"file" | "folder" | null>(
    null,
  );
  const [renamePending, setRenamePending] = useState(false);
  const [renameProgress, setRenameProgress] =
    useState<TaskProgress<FileProgress> | null>(null);
  const toast = useScopedToast(FILES_TOAST_META);
  const navigate = useNavigate();
  const router = useRouter();
  const { startCompression, startExtraction, startCopy, startMove } =
    useBackgroundTaskActions();

  const invalidateListing = useListingInvalidation(normalizedPath);

  const createFileMutation = linuxio.filebrowser.resource_post.useMutation({
    success: () => {
      invalidateListing();
      toast.success("File created successfully");
    },
    error: (error) =>
      toastFileError(
        toast,
        navigate,
        actionErrorMessage(error, "Failed to create file"),
      ),
  });

  const createFile = useCallback(
    async (fileName: string) => {
      if (createPending !== null) return;
      const path = joinPath(normalizedPath, fileName);
      setCreatePending("file");
      await createFileMutation
        .mutateAsync({ path })
        .finally(() => setCreatePending(null));
    },
    [createFileMutation, createPending, normalizedPath],
  );

  const createFolderMutation = linuxio.filebrowser.resource_post.useMutation({
    success: () => {
      invalidateListing();
      toast.success("Folder created successfully");
    },
    error: (error) =>
      toastFileError(
        toast,
        navigate,
        actionErrorMessage(error, "Failed to create folder"),
      ),
  });

  const createFolder = useCallback(
    async (folderName: string) => {
      if (createPending !== null) return;
      const path = `${joinPath(normalizedPath, folderName)}/`;
      setCreatePending("folder");
      await createFolderMutation
        .mutateAsync({ path })
        .finally(() => setCreatePending(null));
    },
    [createFolderMutation, createPending, normalizedPath],
  );

  // The listing changed even when some items failed, so refresh it either
  // way; failures are named in the error toast. Only the job's own paths
  // leave the selection: other items may have been selected, and other jobs
  // started, while it ran.
  const reportRemoval = (
    result: FileBatchResult | undefined,
    verb: "delete" | "trash",
    paths: string[],
  ) => {
    invalidateListing();
    onDeleteSuccess?.(paths);
    const allFailed = result?.failed ?? [];
    // An item whose disk cannot hold a trash is not a failure to report: the
    // user is asked once, for this job, whether to delete those for good
    // (NoTrashPrompt), and they stay where they are until then.
    const noTrash =
      verb === "trash"
        ? allFailed.filter((item) => item.reason === "no_trash")
        : [];
    const failed = noTrash.length
      ? allFailed.filter((item) => item.reason !== "no_trash")
      : allFailed;
    askToDeleteForGood(noTrash.map((item) => item.path));
    if (failed.length > 0) {
      const count = `${failed.length} item${failed.length === 1 ? "" : "s"}`;
      toastFileError(
        toast,
        navigate,
        verb === "trash"
          ? `Failed to move ${count} to Trash`
          : `Failed to delete ${count}`,
        batchFailureOpts(failed),
      );
      return;
    }
    const moved = result?.succeeded ?? 0;
    // Only the question is left to show when nothing went to the Trash.
    if (noTrash.length > 0 && moved === 0) return;
    toast.success(
      verb === "trash"
        ? `Moved ${moved} item${moved === 1 ? "" : "s"} to Trash`
        : "Items deleted successfully",
    );
  };

  // One batch task deletes the whole selection; the bridge loops server-side
  // and reports per-item failures in the result. Its confirm closes as it
  // starts, and any number of these jobs can run at once: each one's result
  // is reported here with its own request, so the task tray, which shows its
  // progress and can cancel it, does not report it as well. A cancel is not
  // a failure: the tray that cancelled it says so.
  const deleteBatchAction = linuxio.filebrowser.delete_batch.useMutation({
    // invalidateListing below is more precise than the manifest entry.
    invalidates: [],
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    success: (result, request) =>
      reportRemoval(result, "delete", request.paths),
    error: (error, request) => {
      // A cancelled job has removed some of its items already: the listing
      // is read again and none of them stays selected.
      if (isTaskCancellationError(error)) {
        invalidateListing();
        onDeleteSuccess?.(request.paths);
        return;
      }
      toastFileError(
        toast,
        navigate,
        getMutationErrorMessage(error, "Failed to delete items"),
      );
    },
  });

  // The mutation object is new on every render; its mutateAsync is not, so
  // the callbacks below keep their identity for the listing and header.
  const { mutateAsync: startDelete } = deleteBatchAction;
  const deleteItems = useCallback(
    async (paths: string[]): Promise<FileBatchResult | undefined> => {
      if (!paths.length) return;
      onDeleteSuccess?.(paths);
      return startDelete({ paths });
    },
    [startDelete, onDeleteSuccess],
  );

  // Same shape as delete, but moves into the FreeDesktop trash.
  const trashBatchAction = linuxio.filebrowser.trash_batch.useMutation({
    // invalidateListing below is more precise than the manifest entry for
    // the folder; the Trash's own list is read again as well.
    invalidates: [TRASH_LIST_KEY],
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    success: (result, request) => reportRemoval(result, "trash", request.paths),
    error: (error, request) => {
      if (isTaskCancellationError(error)) {
        invalidateListing();
        onDeleteSuccess?.(request.paths);
        return;
      }
      toastFileError(
        toast,
        navigate,
        getMutationErrorMessage(error, "Failed to move items to Trash"),
      );
    },
  });

  const { mutateAsync: startTrash } = trashBatchAction;
  const trashItems = useCallback(
    async (paths: string[]): Promise<FileBatchResult | undefined> => {
      if (!paths.length) return;
      onDeleteSuccess?.(paths);
      return startTrash({ paths });
    },
    [startTrash, onDeleteSuccess],
  );

  const compressItems = useCallback(
    async ({ paths, archiveName, destination }: CompressPayload) => {
      if (!paths.length) {
        throw new Error("No paths provided for compression");
      }
      // Pass invalidateListing as onComplete - called when stream actually completes
      await startCompression({
        paths,
        archiveName: archiveName || "archive.zip",
        destination: destination || normalizedPath,
        onComplete: invalidateListing,
      });
    },
    [invalidateListing, normalizedPath, startCompression],
  );

  const extractArchive = useCallback(
    async ({ archivePath, destination }: ExtractPayload) => {
      if (!archivePath) {
        throw new Error("No archive selected");
      }
      try {
        // Pass invalidateListing as onComplete - called when stream actually completes
        await startExtraction({
          archivePath,
          destination,
          onComplete: invalidateListing,
        });
      } catch (error) {
        // Note: errors are also handled by BackgroundTasksContext
        toastFileError(
          toast,
          navigate,
          getMutationErrorMessage(error, "Failed to extract archive"),
        );
        throw error;
      }
    },
    [invalidateListing, navigate, startExtraction, toast],
  );

  // "Try again" on a partial failure's toast opens the Permissions dialog for
  // the failed items with the settings the change was made with. The dialog
  // lives in the file browser page, so it is offered only while that page
  // shows. A failure through a refused link offers its real folder instead
  // (toastFileError).
  const retryPermissionsOption = (
    request: FileChmodBatchRequest,
    paths: string[],
  ) => {
    const { pathname } = router.state.location;
    const onFileBrowser =
      pathname === "/filebrowser" || pathname.startsWith("/filebrowser/");
    if (!onPermissionsRetry || !onFileBrowser) return {};
    const reopen = () =>
      onPermissionsRetry({
        group: request.group || undefined,
        // A recursive change was made on a folder, and a folder's path ends
        // in a slash: either offers "Apply recursively" again.
        isDirectory: Boolean(request.recursive) || paths.some(isDirectoryPath),
        mode: request.mode,
        owner: request.owner || undefined,
        pathLabel: paths.length > 1 ? `${paths.length} items` : paths[0],
        paths,
        recursive: Boolean(request.recursive),
        selectionCount: paths.length,
      });
    // A toast with a button stays long enough to be clicked.
    return {
      action: { label: "Try again", onClick: reopen },
      duration: 10_000,
    };
  };

  // One batch task changes permissions of the whole selection; the bridge
  // loops server-side and reports per-item failures in the result. As for a
  // delete, its dialog closes as it starts, several can run at once, and each
  // result is reported here with its own request, so the task tray does not
  // report it as well, and a cancel from the tray is not a failure.
  const changePermissionsAction = linuxio.filebrowser.chmod_batch.useMutation({
    // invalidateListing below is more precise than the manifest entry.
    invalidates: [],
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    success: (result, request) => {
      invalidateListing();
      const failed = result?.failed ?? [];
      if (failed.length > 0) {
        toastFileError(
          toast,
          navigate,
          `Failed to change permissions on ${failed.length} of ${result?.total ?? failed.length} items`,
          {
            ...batchFailureOpts(failed),
            ...retryPermissionsOption(
              request,
              failed.map((item) => item.path),
            ),
          },
          onPermissionsLinkOpen,
        );
        return;
      }
      toast.success("Permissions changed successfully");
    },
    error: (error) => {
      if (isTaskCancellationError(error)) return;
      toastFileError(
        toast,
        navigate,
        getMutationErrorMessage(error, "Failed to change permissions"),
        undefined,
        onPermissionsLinkOpen,
      );
    },
  });

  const changePermissions = useCallback(
    async ({ paths, mode, recursive, owner, group }: ChmodPayload) => {
      if (!paths.length) {
        throw new Error("No paths provided");
      }
      if (!mode) {
        throw new Error("No mode provided");
      }
      const request: FileChmodBatchRequest = {
        paths,
        mode,
        owner: owner || "",
        group: group || "",
        recursive: recursive || undefined,
      };
      return changePermissionsAction.mutateAsync(request);
    },
    [changePermissionsAction],
  );

  // Renames are usually an atomic rename(2), but the backend falls back to
  // copy+delete and reports byte progress; surface it for that slow path.
  const renameMutation = linuxio.filebrowser.resource_patch.useMutation({
    onProgress: setRenameProgress,
    success: () => {
      setRenameProgress(null);
      invalidateListing();
      toast.success("Item renamed successfully");
    },
    error: (error) => {
      setRenameProgress(null);
      toastFileError(
        toast,
        navigate,
        getMutationErrorMessage(error, "Failed to rename item"),
      );
    },
  });
  // TanStack Query mutation result objects are recreated on renders. Keep the
  // callback dependency scoped to the stable mutation function so unrelated
  // parent updates (for example, selection changes) do not invalidate every
  // directory item's rename handler.
  const { mutateAsync: renameAsync } = renameMutation;

  const renameItem = useCallback(
    async ({ from, destination }: RenamePayload) => {
      if (!from || !destination) {
        throw new Error("Invalid rename parameters");
      }
      const request: ActionSourceDestinationRequest = {
        action: "rename",
        src: from,
        dst: destination,
      };
      if (renamePending) return;
      setRenamePending(true);
      setRenameProgress(null);
      await renameAsync(request).finally(() => setRenamePending(false));
    },
    [renameAsync, renamePending],
  );

  // Transfers never overwrite silently: pre-check the landing paths and ask
  // the user per collision (overwrite/skip/keep both). Returns the sources to
  // transfer, whether any overwrite was chosen and which sources to keep under
  // a unique name; throws CONFLICT_PROMPT_CANCELLED
  // when the user dismissed the prompt, and returns null when every item was
  // skipped (nothing to do). Items already in destinationDir are a no-op for a
  // move; a copy keeps them (the bridge names the copy "name (copy)") but
  // never asks to overwrite an item with itself.
  const resolvePasteCollisions = useCallback(
    async (
      sourcePaths: string[],
      destinationDir: string,
      action: "copy" | "move",
    ): Promise<{
      sources: string[];
      overwrite: boolean;
      keepBoth: string[];
    } | null> => {
      const destination = stripTrailingSlash(destinationDir);
      const isSameFolder = (path: string) => parentPath(path) === destination;
      const candidates =
        action === "move"
          ? sourcePaths.filter((path) => !isSameFolder(path))
          : sourcePaths;
      if (!candidates.length) {
        toast.info("Items are already in this folder");
        return null;
      }
      const checked = candidates.filter((path) => !isSameFolder(path));
      if (!resolveCollisions || !checked.length) {
        return { sources: candidates, overwrite: false, keepBoth: [] };
      }
      // The incoming side of the prompt: size and time of the sources. Best
      // effort — the prompt still works without them.
      const sourceInfo = new Map<string, { size: number; modified: string }>();
      try {
        const response = await call(linuxio.filebrowser.exists_batch.route, {
          paths: checked,
        });
        for (const entry of response.existing ?? []) {
          sourceInfo.set(entry.path, entry);
        }
      } catch {
        // Fall through with no incoming details.
      }
      const resolution = await resolveCollisions(
        checked,
        (path) => joinPath(destinationDir, baseName(path)),
        destinationDir,
        (path) => sourceInfo.get(path),
      );
      if (!resolution) {
        throw CONFLICT_PROMPT_CANCELLED;
      }
      const kept = new Set(resolution.kept);
      const sources = candidates.filter(
        (path) => kept.has(path) || isSameFolder(path),
      );
      if (!sources.length) {
        toast.info("All items skipped");
        return null;
      }
      return {
        sources,
        overwrite: resolution.overwrite,
        keepBoth: resolution.keepBoth,
      };
    },
    [resolveCollisions, toast],
  );

  const copyItems = useCallback(
    async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      if (!sourcePaths.length) {
        throw new Error("No paths provided");
      }
      try {
        const plan = await resolvePasteCollisions(
          sourcePaths,
          destinationDir,
          "copy",
        );
        if (!plan) {
          return;
        }
        // One batch task copies the whole selection into destinationDir; the
        // bridge loops server-side and reports one aggregate progress bar.
        await startCopy({
          sources: plan.sources,
          destination: destinationDir,
          overwrite: plan.overwrite || undefined,
          keepBoth: plan.keepBoth.length ? plan.keepBoth : undefined,
          onComplete: invalidateListing,
        });
      } catch (error) {
        if (error !== CONFLICT_PROMPT_CANCELLED) {
          toastFileError(
            toast,
            navigate,
            getMutationErrorMessage(error, "Failed to copy items"),
          );
        }
        throw error;
      }
    },
    [invalidateListing, navigate, resolvePasteCollisions, startCopy, toast],
  );

  const moveItems = useCallback(
    async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      if (!sourcePaths.length) {
        throw new Error("No paths provided");
      }
      try {
        const plan = await resolvePasteCollisions(
          sourcePaths,
          destinationDir,
          "move",
        );
        if (!plan) {
          return;
        }
        // One batch task moves the whole selection into destinationDir.
        await startMove({
          sources: plan.sources,
          destination: destinationDir,
          overwrite: plan.overwrite || undefined,
          keepBoth: plan.keepBoth.length ? plan.keepBoth : undefined,
          onComplete: invalidateListing,
        });
      } catch (error) {
        if (error !== CONFLICT_PROMPT_CANCELLED) {
          toastFileError(
            toast,
            navigate,
            getMutationErrorMessage(error, "Failed to move items"),
          );
        }
        throw error;
      }
    },
    [invalidateListing, navigate, resolvePasteCollisions, startMove, toast],
  );

  return {
    createFile,
    createPending,
    createFolder,
    deleteItems,
    trashItems,
    compressItems,
    extractArchive,
    changePermissions,
    copyItems,
    moveItems,
    renameItem,
    renamePending,
    renameProgress,
  };
};
