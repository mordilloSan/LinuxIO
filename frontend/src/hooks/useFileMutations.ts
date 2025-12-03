import {
  useMutation,
  useQueryClient,
  QueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";

import axios from "@/utils/axios";

type UseFileMutationsParams = {
  normalizedPath: string;
  queryClient?: QueryClient;
  onDeleteSuccess?: () => void;
};

export const useFileMutations = ({
  normalizedPath,
  queryClient: providedQueryClient,
  onDeleteSuccess,
}: UseFileMutationsParams) => {
  const queryClient = providedQueryClient ?? useQueryClient();

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

  return { createFile, createFolder, deleteItems };
};
