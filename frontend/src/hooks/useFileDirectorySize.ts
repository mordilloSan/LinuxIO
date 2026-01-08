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

interface UseDirectorySizeResult {
  size: number | null;
  isLoading: boolean;
  error: Error | null;
  isUnavailable: boolean;
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

  const { data, isLoading, error } = linuxio.filebrowser.dir_size.useQuery(
    path,
    {
      enabled: queryEnabled,
      ...getDirectorySizeQueryOptions(),
    },
  );

  // Handle indexer unavailability errors
  useIndexerErrorHandler(error);

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
    isLoading: queryEnabled ? isLoading : false,
    error: derivedError,
    isUnavailable,
  };
};
