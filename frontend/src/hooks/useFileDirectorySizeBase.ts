import { useEffect } from "react";
import { LinuxIOError } from "@/api/linuxio";
import useAuth from "@/hooks/useAuth";

/**
 * Shared constants for directory size queries
 */
export const DIRECTORY_SIZE_CONFIG = {
  CACHE_DURATION: 5 * 60 * 1000, // 5 minutes (staleTime)
  CACHE_PERSISTENCE: 24 * 60 * 60 * 1000, // 24 hours (gcTime)
  FAILED_RETRY_DELAY: 30 * 1000, // 30 seconds
  MAX_RETRIES: 2,
  EXCLUDED_DIRECTORIES: ["/proc", "/dev", "/sys"],
} as const;

/**
 * Check if a directory should skip size calculation
 * (system directories not indexed by the indexer)
 */
export const shouldSkipSizeCalculation = (path: string): boolean => {
  if (!path) return true;
  return DIRECTORY_SIZE_CONFIG.EXCLUDED_DIRECTORIES.some(
    (excluded) => path === excluded || path.startsWith(excluded + "/"),
  );
};

/**
 * Common query options for directory size queries
 */
export const getDirectorySizeQueryOptions = () => ({
  staleTime: DIRECTORY_SIZE_CONFIG.CACHE_DURATION,
  gcTime: DIRECTORY_SIZE_CONFIG.CACHE_PERSISTENCE,
  refetchOnWindowFocus: false,
  refetchOnMount: false,
  retry: (failureCount: number) =>
    failureCount < DIRECTORY_SIZE_CONFIG.MAX_RETRIES,
  retryDelay: () => DIRECTORY_SIZE_CONFIG.FAILED_RETRY_DELAY,
});

/**
 * Hook to handle indexer unavailability errors
 * Note: Indexer availability is now managed by AuthContext from login response
 */
export const useIndexerErrorHandler = (error: Error | null) => {
  useEffect(() => {
    if (!error) return;
    if (
      error instanceof LinuxIOError &&
      error.message?.includes("indexer unavailable")
    ) {
      // Indexer availability is now managed by AuthContext from login response
      // No client-side flag setting needed
    }
  }, [error]);
};

/**
 * Get derived error for directory size queries
 */
export const getDirectorySizeError = (
  error: Error | null,
  indexerDisabled: boolean,
  shouldSkip: boolean,
): Error | null => {
  if (indexerDisabled && !shouldSkip) {
    return new Error("Directory size indexing is unavailable");
  }
  return error instanceof Error ? error : null;
};

/**
 * Check if directory size feature is unavailable
 */
export const isDirectorySizeUnavailable = (
  error: Error | null,
  data: any,
  indexerDisabled: boolean,
  shouldSkip: boolean,
): boolean => {
  const derivedError = getDirectorySizeError(
    error,
    indexerDisabled,
    shouldSkip,
  );
  return (derivedError !== null && !data) || (indexerDisabled && !shouldSkip);
};

/**
 * Check if query should be enabled for directory size
 */
export const shouldEnableDirectorySizeQuery = (
  enabled: boolean,
  path: string | null | undefined,
  shouldSkip: boolean,
  indexerDisabled: boolean,
): boolean => {
  return enabled && !!path && !shouldSkip && !indexerDisabled;
};

/**
 * Get the current indexer availability status from AuthContext
 */
export const useIndexerAvailability = () => {
  const { indexerAvailable } = useAuth();
  return indexerAvailable === false;
};
