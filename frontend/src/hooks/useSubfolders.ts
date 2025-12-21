import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { useStreamMux } from "@/hooks/useStreamMux";
import { streamApi, StreamApiError } from "@/utils/streamApi";
import {
  getIndexerAvailabilityFlag,
  setIndexerAvailabilityFlag,
} from "@/utils/indexerAvailability";

export interface SubfolderData {
  path: string;
  name: string;
  size: number;
  mod_time: string;
}

export interface SubfoldersResponse {
  path: string;
  subfolders: SubfolderData[];
  count: number;
}

interface UseSubfoldersResult {
  subfolders: SubfolderData[];
  subfoldersMap: Map<string, SubfolderData>;
  isLoading: boolean;
  error: Error | null;
  isUnavailable: boolean;
}

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes (staleTime)
const CACHE_PERSISTENCE = 24 * 60 * 60 * 1000; // 24 hours (gcTime - keep in cache)
const FAILED_RETRY_DELAY = 30 * 1000; // 30 seconds before retrying failed paths
const MAX_RETRIES = 2;

// Directories that should not have size calculations (not indexed by the indexer)
const EXCLUDED_DIRECTORIES = ["/proc", "/dev", "/sys"];

const shouldSkipSizeCalculation = (path: string): boolean => {
  if (!path) return true;
  return EXCLUDED_DIRECTORIES.some(
    (excluded) => path === excluded || path.startsWith(excluded + "/"),
  );
};

/**
 * Hook to fetch all direct child folders with their sizes for a given path.
 * This replaces making multiple individual dir-size calls.
 *
 * @param path - The parent directory path
 * @param enabled - Whether the query should run
 * @returns Subfolders array and a map for quick lookup by path
 */
export const useSubfolders = (
  path: string,
  enabled: boolean = true,
): UseSubfoldersResult => {
  const { isOpen } = useStreamMux();
  // Skip size calculation for system directories
  const shouldSkip = shouldSkipSizeCalculation(path);
  const indexerDisabled = getIndexerAvailabilityFlag() === false;
  const queryEnabled =
    isOpen && enabled && !!path && !shouldSkip && !indexerDisabled;

  const { data, isLoading, error } = useQuery({
    queryKey: ["stream", "filebrowser", "subfolders", path],
    queryFn: async () => {
      // Args: [path]
      const data = await streamApi.get<SubfoldersResponse>(
        "filebrowser",
        "subfolders",
        [path],
      );
      return data;
    },
    enabled: queryEnabled,
    staleTime: CACHE_DURATION,
    gcTime: CACHE_PERSISTENCE,
    refetchOnWindowFocus: false,
    refetchOnMount: false,
    retry: (failureCount) => failureCount < MAX_RETRIES,
    retryDelay: () => FAILED_RETRY_DELAY,
  });

  useEffect(() => {
    if (!error) return;
    if (
      error instanceof StreamApiError &&
      error.message?.includes("indexer unavailable")
    ) {
      setIndexerAvailabilityFlag(false);
    }
  }, [error]);

  // Create a map for quick lookup by path
  const subfoldersMap = new Map<string, SubfolderData>();
  if (data?.subfolders) {
    data.subfolders.forEach((subfolder) => {
      subfoldersMap.set(subfolder.path, subfolder);
    });
  }

  const derivedError =
    indexerDisabled && !shouldSkip
      ? new Error("Directory size indexing is unavailable")
      : error instanceof Error
        ? error
        : null;
  const isUnavailable =
    (derivedError !== null && !data) || (indexerDisabled && !shouldSkip);

  return {
    subfolders: data?.subfolders ?? [],
    subfoldersMap,
    isLoading: queryEnabled ? isLoading : false,
    error: derivedError,
    isUnavailable,
  };
};

// Function to clear the subfolder cache
export const clearSubfoldersCache = (
  queryClient?: ReturnType<typeof useQueryClient>,
) => {
  if (queryClient) {
    queryClient.removeQueries({
      queryKey: ["stream", "filebrowser", "subfolders"],
    });
  }
};

// Function to clear a specific path from cache
export const clearSubfoldersCacheForPath = (
  path: string,
  queryClient?: ReturnType<typeof useQueryClient>,
) => {
  if (queryClient) {
    queryClient.removeQueries({
      queryKey: ["stream", "filebrowser", "subfolders", path],
    });
  }
};

// Helper function to get subfolder size from the map
export const getSubfolderSize = (
  subfoldersMap: Map<string, SubfolderData>,
  folderPath: string,
): number | null => {
  const subfolder = subfoldersMap.get(folderPath);
  return subfolder ? subfolder.size : null;
};
