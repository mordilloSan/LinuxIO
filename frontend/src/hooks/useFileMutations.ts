import {
  useMutation,
  useQueryClient,
  QueryClient,
} from "@tanstack/react-query";
import { useCallback } from "react";
import { toast } from "sonner";

import { clearFileSubfoldersCache } from "@/hooks/useFileSubfolders";
import linuxio from "@/api/react-query";
import { useFileTransfers } from "./useFileTransfers";
import { getMutationErrorMessage } from "@/utils/mutations";

interface UseFileMutationsParams {
  normalizedPath: string;
  queryClient?: QueryClient;
  onDeleteSuccess?: () => void;
}

interface CompressPayload {
  paths: string[];
  archiveName?: string;
  destination?: string;
}

interface ExtractPayload {
  archivePath: string;
  destination?: string;
}

interface ChmodPayload {
  path: string;
  mode: string;
  recursive?: boolean;
  owner?: string;
  group?: string;
}

interface CopyMovePayload {
  sourcePaths: string[];
  destinationDir: string;
}

interface RenamePayload {
  from: string;
  destination: string;
}

export const useFileMutations = ({
  normalizedPath,
  queryClient: providedQueryClient,
  onDeleteSuccess,
}: UseFileMutationsParams) => {
  const queryClient = providedQueryClient ?? useQueryClient();
  const { startCompression, startExtraction, startCopy, startMove } =
    useFileTransfers();

  const invalidateListing = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: ["linuxio", "filebrowser", "resource_get", normalizedPath],
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
      // Args: [path] - file path without trailing slash
      createFileMutation.mutate([path]);
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
      // Args: [path] - directory path with trailing slash
      createFolderMutation.mutate([path]);
    },
    [createFolderMutation, normalizedPath],
  );

  const deleteItemMutation = linuxio.filebrowser.resource_delete.useMutation();

  const { mutate: deleteItems } = useMutation({
    mutationFn: async (paths: string[]) => {
      await Promise.all(
        paths.map((path) => deleteItemMutation.mutateAsync([path])),
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
      // Note: errors are also handled by FileTransferContext
      toast.error(getMutationErrorMessage(error, "Failed to extract archive"));
    },
  });

  const chmodMutation = linuxio.filebrowser.chmod.useMutation({
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
    async ({ path, mode, recursive, owner, group }: ChmodPayload) => {
      if (!path) {
        throw new Error("No path provided");
      }
      if (!mode) {
        throw new Error("No mode provided");
      }
      // Args: [path, mode, owner?, group?, recursive?]
      const args = [path, mode, owner || "", group || ""];
      if (recursive) {
        args.push("true");
      }
      await chmodMutation.mutateAsync(args);
    },
    [chmodMutation],
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
      // Args: [action, from, destination]
      await renameMutation.mutateAsync(["rename", from, destination]);
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
