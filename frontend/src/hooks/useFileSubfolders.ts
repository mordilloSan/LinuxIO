import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import linuxio from "@/api/react-query";
import {
  shouldSkipSizeCalculation,
  getDirectorySizeQueryOptions,
  useIndexerErrorHandler,
  getDirectorySizeError,
  isDirectorySizeUnavailable,
  shouldEnableDirectorySizeQuery,
  useIndexerAvailability,
} from "./useFileDirectorySizeBase";

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

/**
 * Hook to fetch all direct child folders with their sizes for a given path.
 * This replaces making multiple individual dir-size calls.
 *
 * @param path - The parent directory path
 * @param enabled - Whether the query should run
 * @returns Subfolders array and a map for quick lookup by path
 */
export const useFileSubfolders = (
  path: string,
  enabled: boolean = true,
): UseSubfoldersResult => {
  // Skip size calculation for system directories
  const shouldSkip = shouldSkipSizeCalculation(path);
  const indexerDisabled = useIndexerAvailability();
  const queryEnabled = shouldEnableDirectorySizeQuery(
    enabled,
    path,
    shouldSkip,
    indexerDisabled,
  );

  const { data, isLoading, error } = linuxio.useCall<SubfoldersResponse>(
    "filebrowser",
    "subfolders",
    [path],
    {
      enabled: queryEnabled,
      ...getDirectorySizeQueryOptions(),
    },
  );

  // Handle indexer unavailability errors
  useIndexerErrorHandler(error);

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

  const derivedError = getDirectorySizeError(
    error,
    indexerDisabled,
    shouldSkip,
  );

  const isUnavailable = isDirectorySizeUnavailable(
    error,
    data,
    indexerDisabled,
    shouldSkip,
  );

  return {
    subfolders,
    subfoldersMap,
    isLoading: queryEnabled ? isLoading : false,
    error: derivedError,
    isUnavailable,
  };
};

// Function to clear the subfolder cache
export const clearFileSubfoldersCache = (
  queryClient?: ReturnType<typeof useQueryClient>,
) => {
  if (queryClient) {
    queryClient.removeQueries({
      queryKey: ["stream", "filebrowser", "subfolders"],
    });
  }
};

// Function to clear a specific path from cache
export const clearFileSubfoldersCacheForPath = (
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
