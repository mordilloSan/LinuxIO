import { useQuery, useQueryClient } from "@tanstack/react-query";
import axios from "../utils/axios";

export interface DirectorySizeData {
  path: string;
  size: number;
  fileCount: number;
  folderCount: number;
}

interface UseDirectorySizeResult {
  size: number | null;
  isLoading: boolean;
  error: Error | null;
  isUnavailable: boolean;
}

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes
const FAILED_RETRY_DELAY = 30 * 1000; // 30 seconds before retrying failed paths
const MAX_RETRIES = 2;

export const useDirectorySize = (
  path: string,
  enabled: boolean = true,
): UseDirectorySizeResult => {
  const { data, isLoading, error } = useQuery({
    queryKey: ["directorySize", path],
    queryFn: async () => {
      const response = await axios.get<DirectorySizeData>(
        "/navigator/api/dir-size",
        {
          params: { path },
          timeout: 10000, // 10 second timeout
        },
      );
      return response.data;
    },
    enabled: enabled && !!path,
    staleTime: CACHE_DURATION,
    retry: (failureCount) => failureCount < MAX_RETRIES,
    retryDelay: () => FAILED_RETRY_DELAY,
  });

  const isUnavailable = error !== null && !data;

  return {
    size: data?.size ?? null,
    isLoading,
    error: error instanceof Error ? error : null,
    isUnavailable,
  };
};

// Function to clear the entire directory size cache
export const clearDirectorySizeCache = (queryClient?: ReturnType<typeof useQueryClient>) => {
  if (queryClient) {
    queryClient.removeQueries({ queryKey: ["directorySize"] });
  }
};

// Function to clear a specific path from cache
export const clearDirectorySizeCacheForPath = (
  path: string,
  queryClient?: ReturnType<typeof useQueryClient>,
) => {
  if (queryClient) {
    queryClient.removeQueries({ queryKey: ["directorySize", path] });
  }
};
