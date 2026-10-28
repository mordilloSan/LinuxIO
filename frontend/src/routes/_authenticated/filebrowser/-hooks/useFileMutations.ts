import { useCallback, useState } from "react";

import {
  type ActionSourceDestinationRequest,
  call,
  type CountProgress,
  type FileBatchResult,
  type FileChmodBatchRequest,
  type FileExtractRequest,
  type FileProgress,
  getMutationErrorMessage,
  linuxio,
  type TaskProgress,
} from "@/api";
import { useScopedToast } from "@/hooks/useScopedToast";
import { describeBatchFailures } from "@/tasks/batchFailures";
import { useBackgroundTaskActions } from "@/tasks/useBackgroundTaskActions";
import {
  baseName,
  joinPath,
  parentPath,
  stripTrailingSlash,
} from "@/utils/path";

import {
  CONFLICT_PROMPT_CANCELLED,
  type ResolveCollisionsFn,
} from "./useFileConflicts";
import { useListingInvalidation } from "./useListingInvalidation";

export const FILES_TOAST_META = {
  label: "Open files",
  params: { _splat: "" },
  to: "/filebrowser/$",
} as const;

interface UseFileMutationsParams {
  normalizedPath: string;
  onDeleteSuccess?: () => void;
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
  resolveCollisions,
}: UseFileMutationsParams) => {
  const [createPending, setCreatePending] = useState<"file" | "folder" | null>(
    null,
  );
  const [renamePending, setRenamePending] = useState(false);
  const [renameProgress, setRenameProgress] =
    useState<TaskProgress<FileProgress> | null>(null);
  const [deleteProgress, setDeleteProgress] =
    useState<TaskProgress<CountProgress> | null>(null);
  const [trashProgress, setTrashProgress] =
    useState<TaskProgress<CountProgress> | null>(null);
  const [permissionsProgress, setPermissionsProgress] =
    useState<TaskProgress<CountProgress> | null>(null);
  const toast = useScopedToast(FILES_TOAST_META);
  const { startCompression, startExtraction, startCopy, startMove } =
    useBackgroundTaskActions();

  const invalidateListing = useListingInvalidation(normalizedPath);

  const createFileMutation = linuxio.filebrowser.resource_post.useMutation({
    success: () => {
      invalidateListing();
      toast.success("File created successfully");
    },
    error: "Failed to create file",
    toast: FILES_TOAST_META,
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
    error: "Failed to create folder",
    toast: FILES_TOAST_META,
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

  // The listing changed even when some items failed, so refresh it and close
  // the dialog either way; failures are named in the error toast.
  const reportRemoval = (
    result: FileBatchResult | undefined,
    verb: "delete" | "trash",
  ) => {
    invalidateListing();
    onDeleteSuccess?.();
    const failed = result?.failed ?? [];
    if (failed.length > 0) {
      const count = `${failed.length} item${failed.length === 1 ? "" : "s"}`;
      toast.error(
        verb === "trash"
          ? `Failed to move ${count} to Trash`
          : `Failed to delete ${count}`,
        { description: describeBatchFailures(failed) },
      );
      return;
    }
    const moved = result?.succeeded ?? 0;
    toast.success(
      verb === "trash"
        ? `Moved ${moved} item${moved === 1 ? "" : "s"} to Trash`
        : "Items deleted successfully",
    );
  };

  // One batch task deletes the whole selection; the bridge loops server-side
  // and reports per-item failures in the result.
  const deleteBatchAction = linuxio.filebrowser.delete_batch.useMutation({
    // invalidateListing below is more precise than the manifest entry.
    invalidates: [],
    onProgress: setDeleteProgress,
    success: (result) => {
      setDeleteProgress(null);
      reportRemoval(result, "delete");
    },
    error: (error) => {
      setDeleteProgress(null);
      toast.error(getMutationErrorMessage(error, "Failed to delete items"));
    },
  });

  const deleteItems = useCallback(
    async (paths: string[]): Promise<FileBatchResult | undefined> => {
      if (!paths.length || deleteBatchAction.isPending) return;
      setDeleteProgress(null);
      return deleteBatchAction.mutateAsync({ paths });
    },
    [deleteBatchAction],
  );

  // Same shape as delete, but moves into the FreeDesktop trash.
  const trashBatchAction = linuxio.filebrowser.trash_batch.useMutation({
    // invalidateListing below is more precise than the manifest entry.
    invalidates: [],
    onProgress: setTrashProgress,
    success: (result) => {
      setTrashProgress(null);
      reportRemoval(result, "trash");
    },
    error: (error) => {
      setTrashProgress(null);
      toast.error(
        getMutationErrorMessage(error, "Failed to move items to Trash"),
      );
    },
  });

  const trashItems = useCallback(
    async (paths: string[]): Promise<FileBatchResult | undefined> => {
      if (!paths.length || trashBatchAction.isPending) return;
      setTrashProgress(null);
      return trashBatchAction.mutateAsync({ paths });
    },
    [trashBatchAction],
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
        toast.error(
          getMutationErrorMessage(error, "Failed to extract archive"),
        );
        throw error;
      }
    },
    [invalidateListing, startExtraction, toast],
  );

  // One batch task changes permissions of the whole selection; the bridge
  // loops server-side and reports per-item failures in the result.
  const changePermissionsAction = linuxio.filebrowser.chmod_batch.useMutation({
    // invalidateListing below is more precise than the manifest entry.
    invalidates: [],
    onProgress: setPermissionsProgress,
    success: (result) => {
      setPermissionsProgress(null);
      invalidateListing();
      const failed = result?.failed ?? [];
      if (failed.length > 0) {
        toast.error(
          `Failed to change permissions on ${failed.length} of ${result?.total ?? failed.length} items`,
        );
        return;
      }
      toast.success("Permissions changed successfully");
    },
    error: (error) => {
      setPermissionsProgress(null);
      toast.error(
        getMutationErrorMessage(error, "Failed to change permissions"),
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
      setPermissionsProgress(null);
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
      toast.error(getMutationErrorMessage(error, "Failed to rename item"));
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
          toast.error(getMutationErrorMessage(error, "Failed to copy items"));
        }
        throw error;
      }
    },
    [invalidateListing, resolvePasteCollisions, startCopy, toast],
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
          toast.error(getMutationErrorMessage(error, "Failed to move items"));
        }
        throw error;
      }
    },
    [invalidateListing, resolvePasteCollisions, startMove, toast],
  );

  return {
    createFile,
    createPending,
    createFolder,
    deleteItems,
    deletePending: deleteBatchAction.isPending,
    deleteProgress,
    trashItems,
    trashPending: trashBatchAction.isPending,
    trashProgress,
    compressItems,
    extractArchive,
    changePermissions,
    permissionsPending: changePermissionsAction.isPending,
    permissionsProgress,
    copyItems,
    moveItems,
    renameItem,
    renamePending,
    renameProgress,
  };
};
