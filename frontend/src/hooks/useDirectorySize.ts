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

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes (staleTime)
const CACHE_PERSISTENCE = 24 * 60 * 60 * 1000; // 24 hours (gcTime - keep in cache)
const FAILED_RETRY_DELAY = 30 * 1000; // 30 seconds before retrying failed paths
const MAX_RETRIES = 2;

// Directories that should not have size calculations (not indexed by the indexer)
const EXCLUDED_DIRECTORIES = [
  "/proc",
  "/dev",
  "/sys",
];

const shouldSkipSizeCalculation = (path: string): boolean => {
  if (!path) return true;
  return EXCLUDED_DIRECTORIES.some(
    (excluded) => path === excluded || path.startsWith(excluded + "/")
  );
};

export const useDirectorySize = (
  path: string,
  enabled: boolean = true,
): UseDirectorySizeResult => {
  // Skip size calculation for system directories
  const shouldSkip = shouldSkipSizeCalculation(path);

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
    enabled: enabled && !!path && !shouldSkip,
    staleTime: CACHE_DURATION, // Data stays fresh for 5 minutes - no refetch during this time
    gcTime: CACHE_PERSISTENCE, // Keep data in cache for 24 hours even if unused
    refetchOnWindowFocus: false, // Don't refetch when window regains focus
    refetchOnMount: false, // Don't refetch when component mounts if data is fresh
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
export const clearDirectorySizeCache = (
  queryClient?: ReturnType<typeof useQueryClient>,
) => {
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
