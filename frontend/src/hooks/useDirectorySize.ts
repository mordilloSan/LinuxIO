import { useEffect, useState, useCallback, useRef } from "react";
import axios from "axios";

export interface DirectorySizeData {
  path: string;
  size: number;
  fileCount: number;
  folderCount: number;
}

interface CacheEntry {
  data: DirectorySizeData;
  timestamp: number;
}

interface FailedAttempt {
  timestamp: number;
  error: string;
  retryCount: number;
}

// Global cache for directory sizes (shared across component instances)
const sizeCache = new Map<string, CacheEntry>();
const failedCache = new Map<string, FailedAttempt>();
const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes
const FAILED_RETRY_DELAY = 30 * 1000; // 30 seconds before retrying failed paths
const MAX_RETRIES = 2;

interface UseDirectorySizeResult {
  size: number | null;
  isLoading: boolean;
  error: Error | null;
  isUnavailable: boolean;
}

export const useDirectorySize = (
  path: string,
  enabled: boolean = true,
): UseDirectorySizeResult => {
  const [size, setSize] = useState<number | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const fetchSize = useCallback(async () => {
    if (!enabled || !path) {
      return;
    }

    // Check cache first
    const cached = sizeCache.get(path);
    if (cached && Date.now() - cached.timestamp < CACHE_DURATION) {
      setSize(cached.data.size);
      setError(null);
      return;
    }

    // Check if this path has failed recently
    const failed = failedCache.get(path);
    if (failed) {
      const timeSinceFailed = Date.now() - failed.timestamp;
      if (timeSinceFailed < FAILED_RETRY_DELAY) {
        // Too soon to retry
        setError(new Error(failed.error));
        setSize(null);
        return;
      }
      // Enough time has passed, allow retry
      if (failed.retryCount >= MAX_RETRIES) {
        // Max retries exceeded
        setError(new Error(failed.error));
        setSize(null);
        return;
      }
    }

    try {
      setIsLoading(true);
      setError(null);

      const response = await axios.get<DirectorySizeData>(
        "/navigator/api/dir-size",
        {
          params: { path },
          timeout: 10000, // 10 second timeout
        },
      );

      const result = response.data;

      // Cache the result
      sizeCache.set(path, {
        data: result,
        timestamp: Date.now(),
      });

      // Clear failed cache for this path if it was previously failing
      failedCache.delete(path);

      setSize(result.size);
      setError(null);
    } catch (err) {
      const errorMsg =
        err instanceof Error ? err.message : String(err);
      const error = new Error(`Unable to calculate size: ${errorMsg}`);
      setError(error);
      setSize(null);

      // Track failed attempt
      const currentFailed = failedCache.get(path) || {
        timestamp: Date.now(),
        error: errorMsg,
        retryCount: 0,
      };

      failedCache.set(path, {
        timestamp: Date.now(),
        error: errorMsg,
        retryCount: currentFailed.retryCount + 1,
      });

      // Log the failure for debugging
      console.warn(`Failed to fetch size for ${path}:`, errorMsg);
    } finally {
      setIsLoading(false);
    }
  }, [path, enabled]);

  useEffect(() => {
    fetchSize();

    return () => {
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current);
      }
    };
  }, [fetchSize]);

  const isUnavailable = error !== null && size === null;

  return { size, isLoading, error, isUnavailable };
};

// Function to clear the cache (can be called when navigating or refreshing)
export const clearDirectorySizeCache = () => {
  sizeCache.clear();
};

// Function to clear a specific path from cache
export const clearDirectorySizeCacheForPath = (path: string) => {
  sizeCache.delete(path);
};
