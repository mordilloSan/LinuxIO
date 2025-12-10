import {
  useMutation,
  useQueryClient,
  QueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";

import axios from "@/utils/axios";
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

export const useFileMutations = ({
  normalizedPath,
  queryClient: providedQueryClient,
  onDeleteSuccess,
}: UseFileMutationsParams) => {
  const queryClient = providedQueryClient ?? useQueryClient();
  const { startCompression, startExtraction } = useFileTransfers();

  const invalidateListing = () =>
    queryClient.invalidateQueries({
      queryKey: ["fileResource", normalizedPath],
    });

  const { mutate: createFile } = useMutation({
    mutationFn: async (fileName: string) => {
      const path = `${normalizedPath}${normalizedPath.endsWith("/") ? "" : "/"}${fileName}`;
      await axios.post("/navigator/api/resources", null, {
        params: { path, source: "/" },
      });
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("File created successfully");
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || "Failed to create file");
    },
  });

  const { mutate: createFolder } = useMutation({
    mutationFn: async (folderName: string) => {
      const path = `${normalizedPath}${normalizedPath.endsWith("/") ? "" : "/"}${folderName}/`;
      await axios.post("/navigator/api/resources", null, {
        params: { path, source: "/" },
      });
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Folder created successfully");
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || "Failed to create folder");
    },
  });

  const { mutate: deleteItems } = useMutation({
    mutationFn: async (paths: string[]) => {
      await Promise.all(
        paths.map((path) =>
          axios.delete("/navigator/api/resources", {
            params: { path, source: "/" },
          }),
        ),
      );
    },
    onSuccess: () => {
      invalidateListing();
      onDeleteSuccess?.();
      toast.success("Items deleted successfully");
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || "Failed to delete items");
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
      await startCompression({
        paths,
        archiveName: archiveName || "archive.zip",
        destination: destination || normalizedPath,
      });
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Archive created successfully");
    },
  });

  const { mutateAsync: extractArchive, isPending: isExtracting } = useMutation({
    mutationFn: async ({ archivePath, destination }: ExtractPayload) => {
      if (!archivePath) {
        throw new Error("No archive selected");
      }
      await startExtraction({
        archivePath,
        destination,
      });
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Archive extracted successfully");
    },
    onError: (error: any) => {
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
      await axios.post("/navigator/api/chmod", {
        path,
        mode,
        recursive: recursive || false,
        owner,
        group,
      });
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Permissions changed successfully");
    },
    onError: (error: any) => {
      toast.error(
        error.response?.data?.error || "Failed to change permissions",
      );
    },
  });

  const { mutateAsync: copyItems } = useMutation({
    mutationFn: async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      if (!sourcePaths.length) {
        throw new Error("No paths provided");
      }
      await Promise.all(
        sourcePaths.map((sourcePath) => {
          const fileName = sourcePath.split("/").pop() || "";
          const destination = `${destinationDir}${destinationDir.endsWith("/") ? "" : "/"}${fileName}`;
          return axios.patch("/navigator/api/resources", null, {
            params: {
              action: "copy",
              from: sourcePath,
              destination,
            },
          });
        }),
      );
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Items copied successfully");
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || "Failed to copy items");
    },
  });

  const { mutateAsync: moveItems } = useMutation({
    mutationFn: async ({ sourcePaths, destinationDir }: CopyMovePayload) => {
      if (!sourcePaths.length) {
        throw new Error("No paths provided");
      }
      await Promise.all(
        sourcePaths.map((sourcePath) => {
          const fileName = sourcePath.split("/").pop() || "";
          const destination = `${destinationDir}${destinationDir.endsWith("/") ? "" : "/"}${fileName}`;
          return axios.patch("/navigator/api/resources", null, {
            params: {
              action: "move",
              from: sourcePath,
              destination,
            },
          });
        }),
      );
    },
    onSuccess: () => {
      invalidateListing();
      toast.success("Items moved successfully");
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || "Failed to move items");
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
    isCompressing,
    isExtracting,
  };
};
