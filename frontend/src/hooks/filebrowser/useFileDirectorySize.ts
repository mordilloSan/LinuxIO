import { useQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import { stripTrailingSlash } from "@/utils/path";

import {
  getDirectorySizeError,
  getDirectorySizeQueryOptions,
  isDirectorySizeUnavailable,
  shouldEnableDirectorySizeQuery,
  shouldSkipSizeCalculation,
  useIndexerAvailability,
} from "./useFileDirectorySizeBase";

interface UseDirectorySizeResult {
  error: Error | null;
  fileCount: number | null;
  folderCount: number | null;
  isLoading: boolean;
  isUnavailable: boolean;
  size: number | null;
}

/**
 * Hook to fetch the size of a single directory.
 * Uses the same cache as useMultipleDirectoryDetails.
 *
 * @param path - The directory path
 * @param enabled - Whether the query should run
 * @returns Directory size and loading/error states
 */
export const useFileDirectorySize = (
  path: string,
  enabled: boolean = true,
): UseDirectorySizeResult => {
  // Skip size calculation for system directories
  const shouldSkip = shouldSkipSizeCalculation(path);
  const indexerDisabled = useIndexerAvailability();
  const queryEnabled = shouldEnableDirectorySizeQuery(
    enabled,
    path,
    shouldSkip,
    indexerDisabled,
  );

  const { data, isLoading, error } = useQuery({
    ...linuxio.filebrowser.dir_size({ path: stripTrailingSlash(path) }),
    enabled: queryEnabled,
    ...getDirectorySizeQueryOptions(),
  });

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
    size: data?.size ?? null,
    fileCount: data?.fileCount ?? null,
    folderCount: data?.folderCount ?? null,
    isLoading: queryEnabled ? isLoading : false,
    error: derivedError,
    isUnavailable,
  };
};
