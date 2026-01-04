import {
  useMutation,
  useQueryClient,
  QueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";

import { clearFileSubfoldersCache } from "@/hooks/useFileSubfolders";
import { linuxio, LinuxIOError } from "@/api/linuxio";
import { useFileTransfers } from "./useFileTransfers";

type UseFileMutationsParams = {
  normalizedPath: string;
  queryClient?: QueryClient;
  onDeleteSuccess?: () => void;
};

type CompressPayload = {
  paths: string[];
  archiveName?: string;
  destination?: string;
};

type ExtractPayload = {
  archivePath: string;
  destination?: string;
};

type ChmodPayload = {
  path: string;
  mode: string;
  recursive?: boolean;
  owner?: string;
  group?: string;
};

type CopyMovePayload = {
  sourcePaths: string[];
  destinationDir: string;
};

type RenamePayload = {
  from: string;
  destination: string;
};

export const useFileMutations = ({
  normalizedPath,
  queryClient: providedQueryClient,
  onDeleteSuccess,
}: UseFileMutationsParams) => {
  const queryClient = providedQueryClient ?? useQueryClient();
  const { startCompression, startExtraction } = useFileTransfers();

  const invalidateListing = () => {
    queryClient.invalidateQueries({
      queryKey: ["stream", "filebrowser", "resource_get", normalizedPath],
    });
    clearFileSubfoldersCache(queryClient);
  };

  const getErrorMessage = (error: unknown, fallback: string): string => {
    if (error instanceof LinuxIOError) {
      return error.message || fallback;
    }
    if (error instanceof Error) {
      return error.message || fallback;
    }
    return fallback;
  };

  const { mutate: createFile } = useMutation({
    mutationFn: async (fileName: string) => {
      const path = `${normalizedPath}${normalizedPath.endsWith("/") ? "" : "/"}${fileName}`;
      // Args: [path] - file path without trailing slash
      await linuxio.request("filebrowser", "resource_post", [path]);
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("File created successfully");
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to create file"));
    },
  });

  const { mutate: createFolder } = useMutation({
    mutationFn: async (folderName: string) => {
      const path = `${normalizedPath}${normalizedPath.endsWith("/") ? "" : "/"}${folderName}/`;
      // Args: [path] - directory path with trailing slash
      await linuxio.request("filebrowser", "resource_post", [path]);
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Folder created successfully");
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to create folder"));
    },
  });

  const { mutate: deleteItems } = useMutation({
    mutationFn: async (paths: string[]) => {
      await Promise.all(
        paths.map((path) =>
          // Args: [path]
          linuxio.request("filebrowser", "resource_delete", [path]),
        ),
      );
    },
    onSuccess: () => {
      invalidateListing();
      onDeleteSuccess?.();
      toast.success("Items deleted successfully");
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to delete items"));
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
    onError: (error: any) => {
      // Note: errors are also handled by FileTransferContext
      toast.error(error.response?.data?.error || "Failed to extract archive");
    },
  });

  const { mutateAsync: changePermissions } = useMutation({
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
      // Args: [path, mode, owner?, group?, recursive?]
      const args = [path, mode, owner || "", group || ""];
      if (recursive) {
        args.push("true");
      }
      await linuxio.request("filebrowser", "chmod", args);
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Permissions changed successfully");
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to change permissions"));
    },
  });

  const { mutateAsync: renameItem } = useMutation({
    mutationFn: async ({ from, destination }: RenamePayload) => {
      if (!from || !destination) {
        throw new Error("Invalid rename parameters");
      }
      // Args: [action, from, destination]
      await linuxio.request("filebrowser", "resource_patch", [
        "rename",
        from,
        destination,
      ]);
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Item renamed successfully");
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to rename item"));
    },
  });

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
          // Args: [action, from, destination]
          return linuxio.request("filebrowser", "resource_patch", [
            "copy",
            sourcePath,
            destination,
          ]);
        }),
      );
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Items copied successfully");
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to copy items"));
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
          // Args: [action, from, destination]
          return linuxio.request("filebrowser", "resource_patch", [
            "move",
            sourcePath,
            destination,
          ]);
        }),
      );
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Items moved successfully");
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, "Failed to move items"));
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
