import {
  QueryClient,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useCallback } from "react";

import { linuxio, openJobAttachStream } from "@/api";
import { clearFileSubfoldersCache } from "@/hooks/filebrowser/useFileSubfolders";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

import { useBackgroundJobActions } from "../backgroundJobs/useBackgroundJobActions";
import { useStreamResult } from "../useStreamResult";

interface UseFileMutationsParams {
  normalizedPath: string;
  onDeleteSuccess?: () => void;
  queryClient?: QueryClient;
}

interface CompressPayload {
  archiveName?: string;
  destination?: string;
  paths: string[];
}

interface ExtractPayload {
  archivePath: string;
  destination?: string;
}

interface ChmodPayload {
  group?: string;
  mode: string;
  owner?: string;
  path: string;
  recursive?: boolean;
}

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
  queryClient: providedQueryClient,
  onDeleteSuccess,
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
      const path = `${normalizedPath}${normalizedPath.endsWith("/") ? "" : "/"}${fileName}`;
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
      const path = `${normalizedPath}${normalizedPath.endsWith("/") ? "" : "/"}${folderName}/`;
      createFolderMutation.mutate({ path });
    },
    [createFolderMutation, normalizedPath],
  );

  const { mutate: deleteItems } = useMutation({
    mutationFn: async (paths: string[]) => {
      await Promise.all(
        paths.map(async (path) => {
          const job = await linuxio.filebrowser.resource_delete(path);
          await runStreamResult({
            open: () => openJobAttachStream(job.id),
            closeMessage: "Delete job stream closed before completion",
          });
        }),
      );
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
      const job = await linuxio.filebrowser.chmod({
        path,
        mode,
        owner: owner || "",
        group: group || "",
        recursive: recursive || undefined,
      });
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
      await renameMutation.mutateAsync({
        action: "rename",
        src: from,
        dst: destination,
      });
    },
    [renameMutation],
  );

  const { mutateAsync: copyItems } = useMutation({
    mutationFn: async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      if (!sourcePaths.length) {
        throw new Error("No paths provided");
      }
      const cleanBase = (p: string) =>
        (p.replace(/\/+$/, "").split("/").pop() || "").trim();

      await Promise.all(
        sourcePaths.map((sourcePath) => {
          const fileName = cleanBase(sourcePath);
          if (!fileName) {
            throw new Error(`Invalid source path: "${sourcePath}"`);
          }
          const destination = `${destinationDir}${destinationDir.endsWith("/") ? "" : "/"}${fileName}`;
          // Use startCopy for progress tracking
          return startCopy({
            source: sourcePath,
            destination,
            onComplete: invalidateListing,
          });
        }),
      );
    },
    onError: (error: unknown) => {
      toast.error(getMutationErrorMessage(error, "Failed to copy items"));
    },
  });

  const { mutateAsync: moveItems } = useMutation({
    mutationFn: async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      if (!sourcePaths.length) {
        throw new Error("No paths provided");
      }
      const cleanBase = (p: string) =>
        (p.replace(/\/+$/, "").split("/").pop() || "").trim();

      await Promise.all(
        sourcePaths.map((sourcePath) => {
          const fileName = cleanBase(sourcePath);
          if (!fileName) {
            throw new Error(`Invalid source path: "${sourcePath}"`);
          }
          const destination = `${destinationDir}${destinationDir.endsWith("/") ? "" : "/"}${fileName}`;
          // Use startMove for progress tracking
          return startMove({
            source: sourcePath,
            destination,
            onComplete: invalidateListing,
          });
        }),
      );
    },
    onError: (error: unknown) => {
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
