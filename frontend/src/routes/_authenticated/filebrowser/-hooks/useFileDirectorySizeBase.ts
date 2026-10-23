/**
 * Shared constants for directory size queries
 */
export const DIRECTORY_SIZE_CONFIG = {
  CACHE_DURATION: 5 * 60 * 1000, // 5 minutes (staleTime)
  CACHE_PERSISTENCE: 24 * 60 * 60 * 1000, // 24 hours (gcTime)
  FAILED_RETRY_DELAY: 30 * 1000, // 30 seconds
  MAX_RETRIES: 2,
  EXCLUDED_DIRECTORIES: ["/proc", "/dev", "/sys", "/var/lib/linuxio/indexer"],
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
 * Get derived error for directory size queries
 */
export const getDirectorySizeError = (
  error: Error | null,
  shouldSkip: boolean,
): Error | null => {
  if (shouldSkip) return null;
  return error instanceof Error ? error : null;
};

/**
 * Check if directory size feature is unavailable
 */
export const isDirectorySizeUnavailable = (
  error: Error | null,
  shouldSkip: boolean,
): boolean => {
  return getDirectorySizeError(error, shouldSkip) !== null;
};

/**
 * Check if query should be enabled for directory size
 */
export const shouldEnableDirectorySizeQuery = (
  enabled: boolean,
  path: string | null | undefined,
  shouldSkip: boolean,
): boolean => {
  return enabled && !!path && !shouldSkip;
};
