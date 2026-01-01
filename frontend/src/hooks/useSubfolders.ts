import { useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { linuxio, LinuxIOError } from "@/api/linuxio";
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
  // Skip size calculation for system directories
  const shouldSkip = shouldSkipSizeCalculation(path);
  const indexerDisabled = getIndexerAvailabilityFlag() === false;
  const queryEnabled = enabled && !!path && !shouldSkip && !indexerDisabled;

  const { data, isLoading, error } = linuxio.call<SubfoldersResponse>(
    "filebrowser",
    "subfolders",
    [path],
    {
      enabled: queryEnabled,
      staleTime: CACHE_DURATION,
      gcTime: CACHE_PERSISTENCE,
      refetchOnWindowFocus: false,
      refetchOnMount: false,
      retry: (failureCount: number) => failureCount < MAX_RETRIES,
      retryDelay: () => FAILED_RETRY_DELAY,
    },
  );

  useEffect(() => {
    if (!error) return;
    if (
      error instanceof LinuxIOError &&
      error.message?.includes("indexer unavailable")
    ) {
      setIndexerAvailabilityFlag(false);
    }
  }, [error]);

  // Create a stable array reference (avoid new empty array on each render)
  const subfolders = useMemo(() => data?.subfolders ?? [], [data?.subfolders]);

  // Create a memoized map for quick lookup by path
  const subfoldersMap = useMemo(() => {
    const map = new Map<string, SubfolderData>();
    subfolders.forEach((subfolder) => {
      map.set(subfolder.path, subfolder);
    });
    return map;
  }, [subfolders]);

  const derivedError =
    indexerDisabled && !shouldSkip
      ? new Error("Directory size indexing is unavailable")
      : error instanceof Error
        ? error
        : null;
  const isUnavailable =
    (derivedError !== null && !data) || (indexerDisabled && !shouldSkip);

  return {
    subfolders,
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
