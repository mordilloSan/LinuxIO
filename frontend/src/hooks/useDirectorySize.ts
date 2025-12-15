import { useQuery } from "@tanstack/react-query";
import { isAxiosError } from "axios";
import { useEffect } from "react";

import axios from "@/utils/axios";
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
  // Skip size calculation for system directories
  const shouldSkip = shouldSkipSizeCalculation(path);
  const indexerDisabled = getIndexerAvailabilityFlag() === false;
  const queryEnabled = enabled && !!path && !shouldSkip && !indexerDisabled;

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
      isAxiosError(error) &&
      error.response?.data &&
      (error.response.data as any).error === "indexer unavailable"
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
