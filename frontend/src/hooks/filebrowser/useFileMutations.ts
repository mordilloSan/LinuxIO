import { useCallback } from "react";

import {
  type ActionSourceDestinationRequest,
  type FileChmodBatchRequest,
  type FileExtractRequest,
  linuxio,
  useCallMutation,
} from "@/api";
import {
  CONFLICT_PROMPT_CANCELLED,
  type ResolveCollisionsFn,
} from "@/hooks/filebrowser/useFileConflicts";
import { useListingInvalidation } from "@/hooks/filebrowser/useListingInvalidation";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";
import { joinPath } from "@/utils/path";

import { useBackgroundTaskActions } from "../backgroundTasks/useBackgroundTaskActions";

const FILES_TOAST_META = {
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
  const toast = useScopedToast(FILES_TOAST_META);
  const { startCompression, startExtraction, startCopy, startMove } =
    useBackgroundTaskActions();

  const invalidateListing = useListingInvalidation(normalizedPath);

  const createFileMutation = useCallMutation(
    linuxio.filebrowser.resource_post,
    {
      success: () => {
        invalidateListing();
        toast.success("File created successfully");
      },
      error: "Failed to create file",
      toast: FILES_TOAST_META,
    },
  );

  const createFile = useCallback(
    (fileName: string) => {
      const path = joinPath(normalizedPath, fileName);
      createFileMutation.mutate({ path });
    },
    [createFileMutation, normalizedPath],
  );

  const createFolderMutation = useCallMutation(
    linuxio.filebrowser.resource_post,
    {
      success: () => {
        invalidateListing();
        toast.success("Folder created successfully");
      },
      error: "Failed to create folder",
      toast: FILES_TOAST_META,
    },
  );

  const createFolder = useCallback(
    (folderName: string) => {
      const path = `${joinPath(normalizedPath, folderName)}/`;
      createFolderMutation.mutate({ path });
    },
    [createFolderMutation, normalizedPath],
  );

  // One batch task deletes the whole selection; the bridge loops server-side
  // and reports per-item failures in the result.
  const deleteBatchAction =
    linuxio.filebrowser.delete_batch.useTaskStreamAction({
      closeMessage: "Delete task stream closed before completion",
      // invalidateListing below is more precise than the manifest entry.
      invalidates: [],
      success: (result) => {
        const failed = result?.failed ?? [];
        if (failed.length > 0) {
          toast.error(
            `Failed to delete ${failed.length} item${failed.length === 1 ? "" : "s"}`,
          );
          return;
        }
        invalidateListing();
        onDeleteSuccess?.();
        toast.success("Items deleted successfully");
      },
      error: (error) => {
        toast.error(getMutationErrorMessage(error, "Failed to delete items"));
      },
    });

  const deleteItems = useCallback(
    (paths: string[]) => {
      if (!paths.length) return;
      deleteBatchAction.mutate({ paths });
    },
    [deleteBatchAction],
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
  const changePermissionsAction =
    linuxio.filebrowser.chmod_batch.useTaskStreamAction({
      closeMessage: "Permissions task stream closed before completion",
      // invalidateListing below is more precise than the manifest entry.
      invalidates: [],
      success: (result) => {
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
      await changePermissionsAction.mutateAsync(request);
    },
    [changePermissionsAction],
  );

  const renameMutation = linuxio.filebrowser.resource_patch.useTaskAction({
    success: () => {
      invalidateListing();
      toast.success("Item renamed successfully");
    },
    error: "Failed to rename item",
    toast: FILES_TOAST_META,
  });

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
      await renameMutation.mutateAsync(request);
    },
    [renameMutation],
  );

  // Transfers never overwrite silently: pre-check the landing paths and ask
  // the user per collision (overwrite/skip). Returns the sources to transfer
  // plus whether any overwrite was chosen; throws CONFLICT_PROMPT_CANCELLED
  // when the user dismissed the prompt, and returns null when every item was
  // skipped (nothing to do).
  const resolvePasteCollisions = useCallback(
    async (
      sourcePaths: string[],
      destinationDir: string,
    ): Promise<{ sources: string[]; overwrite: boolean } | null> => {
      if (!resolveCollisions) {
        return { sources: sourcePaths, overwrite: false };
      }
      const baseName = (path: string) =>
        path.replace(/\/+$/, "").split("/").pop() || path;
      const resolution = await resolveCollisions(
        sourcePaths,
        (path) => joinPath(destinationDir, baseName(path)),
        destinationDir,
      );
      if (!resolution) {
        throw CONFLICT_PROMPT_CANCELLED;
      }
      if (!resolution.kept.length) {
        toast.info("All items skipped");
        return null;
      }
      return { sources: resolution.kept, overwrite: resolution.overwrite };
    },
    [resolveCollisions, toast],
  );

  const copyItems = useCallback(
    async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      try {
        if (!sourcePaths.length) {
          throw new Error("No paths provided");
        }
        const plan = await resolvePasteCollisions(sourcePaths, destinationDir);
        if (!plan) {
          return;
        }
        // One batch task copies the whole selection into destinationDir; the
        // bridge loops server-side and reports one aggregate progress bar.
        await startCopy({
          sources: plan.sources,
          destination: destinationDir,
          overwrite: plan.overwrite || undefined,
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
      try {
        if (!sourcePaths.length) {
          throw new Error("No paths provided");
        }
        const plan = await resolvePasteCollisions(sourcePaths, destinationDir);
        if (!plan) {
          return;
        }
        // One batch task moves the whole selection into destinationDir.
        await startMove({
          sources: plan.sources,
          destination: destinationDir,
          overwrite: plan.overwrite || undefined,
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
    createFolder,
    deleteItems,
    compressItems,
    extractArchive,
    changePermissions,
    copyItems,
    moveItems,
    renameItem,
  };
};
