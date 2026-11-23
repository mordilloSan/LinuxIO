/**
 * Shared constants for directory size queries
 */
export const DIRECTORY_SIZE_CONFIG = {
  CACHE_DURATION: 5 * 60 * 1000, // 5 minutes (staleTime)
  CACHE_PERSISTENCE: 24 * 60 * 60 * 1000, // 24 hours (gcTime)
  FAILED_RETRY_DELAY: 30 * 1000, // 30 seconds
  // How long after an answer marked pending the size is asked for again.
  PENDING_REFETCH_DELAY: 1500,
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

interface PendingSizeQuery {
  state: { data?: { pending?: boolean } };
}

interface MarkedSizeQuery {
  state: { isInvalidated: boolean };
}

/**
 * Common query options for directory size queries.
 *
 * A file operation reports done before the index has caught up with it, so a
 * size asked for right then is the old one. The answer says so (`pending`),
 * and the query asks again shortly after, for as long as it does, instead of
 * keeping that answer for the cache duration.
 *
 * A size is not asked for again merely because a row or a page mounts: it is
 * kept for the cache duration. One that was marked stale while nothing showed
 * it is the exception, since marking it starts no request then: a folder read
 * by the route's loader before the page exists, or changed by an operation
 * that ended while another page was open. A size whose last request failed
 * counts as marked too, so the page asks again when it is next shown.
 */
export const getDirectorySizeQueryOptions = () => ({
  staleTime: DIRECTORY_SIZE_CONFIG.CACHE_DURATION,
  gcTime: DIRECTORY_SIZE_CONFIG.CACHE_PERSISTENCE,
  refetchOnWindowFocus: false,
  refetchOnMount: (query: MarkedSizeQuery) => query.state.isInvalidated,
  refetchInterval: (query: PendingSizeQuery) =>
    query.state.data?.pending
      ? DIRECTORY_SIZE_CONFIG.PENDING_REFETCH_DELAY
      : false,
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
