import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";

import { useStreamMux } from "@/hooks/useStreamMux";
import { streamApi, StreamApiError } from "@/utils/streamApi";
import {
  getIndexerAvailabilityFlag,
  setIndexerAvailabilityFlag,
} from "@/utils/indexerAvailability";

interface DirectorySizeData {
  path: string;
  size: number;
}

interface UseDirectorySizeResult {
  size: number | null;
  isLoading: boolean;
  error: Error | null;
  isUnavailable: boolean;
}

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes
const CACHE_PERSISTENCE = 24 * 60 * 60 * 1000; // 24 hours
const FAILED_RETRY_DELAY = 30 * 1000; // 30 seconds
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
 * Hook to fetch the size of a single directory.
 * Uses the same cache as useMultipleDirectoryDetails.
 *
 * @param path - The directory path
 * @param enabled - Whether the query should run
 * @returns Directory size and loading/error states
 */
export const useDirectorySize = (
  path: string,
  enabled: boolean = true,
): UseDirectorySizeResult => {
  const { isOpen } = useStreamMux();
  // Skip size calculation for system directories
  const shouldSkip = shouldSkipSizeCalculation(path);
  const indexerDisabled = getIndexerAvailabilityFlag() === false;
  const queryEnabled =
    isOpen && enabled && !!path && !shouldSkip && !indexerDisabled;

  const { data, isLoading, error } = useQuery({
    queryKey: ["stream", "filebrowser", "dir_size", path],
    queryFn: async () => {
      // Args: [path]
      const data = await streamApi.get<DirectorySizeData>(
        "filebrowser",
        "dir_size",
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

  const derivedError =
    indexerDisabled && !shouldSkip
      ? new Error("Directory size indexing is unavailable")
      : error instanceof Error
        ? error
        : null;

  const isUnavailable =
    (derivedError !== null && !data) || (indexerDisabled && !shouldSkip);

  return {
    size: data?.size ?? null,
    isLoading: queryEnabled ? isLoading : false,
    error: derivedError,
    isUnavailable,
  };
};
