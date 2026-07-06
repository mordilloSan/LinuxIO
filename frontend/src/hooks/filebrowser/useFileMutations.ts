import {
  QueryClient,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useCallback } from "react";

import {
  type ActionSourceDestinationRequest,
  type FileChmodRequest,
  type FileExtractRequest,
  linuxio,
  openJobAttachStream,
} from "@/api";
import {
  CONFLICT_PROMPT_CANCELLED,
  type ResolveCollisionsFn,
} from "@/hooks/filebrowser/useFileConflicts";
import { clearFileSubfoldersCache } from "@/hooks/filebrowser/useFileSubfolders";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";
import { joinPath } from "@/utils/path";

import { useBackgroundJobActions } from "../backgroundJobs/useBackgroundJobActions";
import { useStreamResult } from "../useStreamResult";

interface UseFileMutationsParams {
  normalizedPath: string;
  onDeleteSuccess?: () => void;
  queryClient?: QueryClient;
  resolveCollisions?: ResolveCollisionsFn;
}

interface CompressPayload {
  archiveName?: string;
  destination?: string;
  paths: string[];
}

type ExtractPayload = FileExtractRequest;

type ChmodPayload = Pick<FileChmodRequest, "mode" | "path" | "recursive"> &
  Partial<Pick<FileChmodRequest, "group" | "owner">>;

interface CopyMovePayload {
  destinationDir: string;
  sourcePaths: string[];
}

interface RenamePayload {
  destination: string;
  from: string;
}

// Result returned by the batch copy/move/delete bridge jobs.
interface BatchJobResult {
  total?: number;
  succeeded?: number;
  failed?: { path: string; error: string }[];
}

export const useFileMutations = ({
  normalizedPath,
  queryClient: providedQueryClient,
  onDeleteSuccess,
  resolveCollisions,
}: UseFileMutationsParams) => {
  const toast = useScopedToast({ href: "/filebrowser", label: "Open files" });
  const queryClient = providedQueryClient ?? useQueryClient();
  const { startCompression, startExtraction, startCopy, startMove } =
    useBackgroundJobActions();
  const { run: runStreamResult } = useStreamResult();

  const invalidateListing = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: linuxio.filebrowser.resource_get.queryKey({
        path: normalizedPath,
      }),
    });
    clearFileSubfoldersCache(queryClient);
  }, [normalizedPath, queryClient]);

  const createFileMutation = linuxio.filebrowser.resource_post.useMutation({
    onSuccess: () => {
      invalidateListing();
      toast.success("File created successfully");
    },
    onError: (error: unknown) => {
      toast.error(getMutationErrorMessage(error, "Failed to create file"));
    },
  });

  const createFile = useCallback(
    (fileName: string) => {
      const path = joinPath(normalizedPath, fileName);
      createFileMutation.mutate({ path });
    },
    [createFileMutation, normalizedPath],
  );

  const createFolderMutation = linuxio.filebrowser.resource_post.useMutation({
    onSuccess: () => {
      invalidateListing();
      toast.success("Folder created successfully");
    },
    onError: (error: unknown) => {
      toast.error(getMutationErrorMessage(error, "Failed to create folder"));
    },
  });

  const createFolder = useCallback(
    (folderName: string) => {
      const path = `${joinPath(normalizedPath, folderName)}/`;
      createFolderMutation.mutate({ path });
    },
    [createFolderMutation, normalizedPath],
  );

  const { mutate: deleteItems } = useMutation({
    mutationFn: async (paths: string[]) => {
      if (!paths.length) return;
      // One batch job deletes the whole selection; the bridge loops
      // server-side and reports per-item failures in the result.
      const job = await linuxio.filebrowser.delete_batch(paths);
      const result = await runStreamResult<BatchJobResult>({
        open: () => openJobAttachStream(job.id),
        closeMessage: "Delete job stream closed before completion",
      });
      const failed = result?.failed ?? [];
      if (failed.length > 0) {
        throw new Error(
          `Failed to delete ${failed.length} item${failed.length === 1 ? "" : "s"}`,
        );
      }
    },
    onSuccess: () => {
      invalidateListing();
      onDeleteSuccess?.();
      toast.success("Items deleted successfully");
    },
    onError: (error: unknown) => {
      toast.error(getMutationErrorMessage(error, "Failed to delete items"));
    },
  });

  const { mutateAsync: compressItems, isPending: isCompressing } = useMutation({
    mutationFn: async ({
      paths,
      archiveName,
      destination,
    }: CompressPayload) => {
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
  });

  const { mutateAsync: extractArchive, isPending: isExtracting } = useMutation({
    mutationFn: async ({ archivePath, destination }: ExtractPayload) => {
      if (!archivePath) {
        throw new Error("No archive selected");
      }
      // Pass invalidateListing as onComplete - called when stream actually completes
      await startExtraction({
        archivePath,
        destination,
        onComplete: invalidateListing,
      });
    },
    onError: (error: unknown) => {
      // Note: errors are also handled by BackgroundJobsContext
      toast.error(getMutationErrorMessage(error, "Failed to extract archive"));
    },
  });

  const { mutateAsync: changePermissionsAsync } = useMutation({
    mutationFn: async ({
      path,
      mode,
      recursive,
      owner,
      group,
    }: ChmodPayload) => {
      if (!path) {
        throw new Error("No path provided");
      }
      if (!mode) {
        throw new Error("No mode provided");
      }
      const request: FileChmodRequest = {
        path,
        mode,
        owner: owner || "",
        group: group || "",
        recursive: recursive || undefined,
      };
      const job = await linuxio.filebrowser.chmod(request);
      await runStreamResult({
        open: () => openJobAttachStream(job.id),
        closeMessage: "Permissions job stream closed before completion",
      });
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Permissions changed successfully");
    },
    onError: (error: unknown) => {
      toast.error(
        getMutationErrorMessage(error, "Failed to change permissions"),
      );
    },
  });

  const changePermissions = useCallback(
    async (payload: ChmodPayload) => {
      await changePermissionsAsync(payload);
    },
    [changePermissionsAsync],
  );

  const renameMutation = linuxio.filebrowser.resource_patch.useMutation({
    onSuccess: () => {
      invalidateListing();
      toast.success("Item renamed successfully");
    },
    onError: (error: unknown) => {
      toast.error(getMutationErrorMessage(error, "Failed to rename item"));
    },
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

  const { mutateAsync: copyItems } = useMutation({
    mutationFn: async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      if (!sourcePaths.length) {
        throw new Error("No paths provided");
      }
      const plan = await resolvePasteCollisions(sourcePaths, destinationDir);
      if (!plan) {
        return;
      }
      // One batch job copies the whole selection into destinationDir; the
      // bridge loops server-side and reports one aggregate progress bar.
      await startCopy({
        sources: plan.sources,
        destination: destinationDir,
        overwrite: plan.overwrite || undefined,
        onComplete: invalidateListing,
      });
    },
    onError: (error: unknown) => {
      if (error === CONFLICT_PROMPT_CANCELLED) {
        return;
      }
      toast.error(getMutationErrorMessage(error, "Failed to copy items"));
    },
  });

  const { mutateAsync: moveItems } = useMutation({
    mutationFn: async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      if (!sourcePaths.length) {
        throw new Error("No paths provided");
      }
      const plan = await resolvePasteCollisions(sourcePaths, destinationDir);
      if (!plan) {
        return;
      }
      // One batch job moves the whole selection into destinationDir.
      await startMove({
        sources: plan.sources,
        destination: destinationDir,
        overwrite: plan.overwrite || undefined,
        onComplete: invalidateListing,
      });
    },
    onError: (error: unknown) => {
      if (error === CONFLICT_PROMPT_CANCELLED) {
        return;
      }
      toast.error(getMutationErrorMessage(error, "Failed to move items"));
    },
  });

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
    isCompressing,
    isExtracting,
  };
};
