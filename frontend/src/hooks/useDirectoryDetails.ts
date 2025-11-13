import { useQuery } from "@tanstack/react-query";
import axios from "../utils/axios";

export interface DirectoryDetailsData {
  path: string;
  size: number;
}

interface UseDirectoryDetailsResult {
  size: number | null;
  isLoading: boolean;
  error: Error | null;
  isUnavailable: boolean;
}

const CACHE_DURATION = 1 * 30 * 1000; // 30 seconds (staleTime)
const CACHE_PERSISTENCE = 24 * 60 * 60 * 1000; // 24 hours (gcTime - keep in cache)
const FAILED_RETRY_DELAY = 30 * 1000; // 30 seconds before retrying failed paths
const MAX_RETRIES = 1;

export const useDirectoryDetails = (
  path: string,
  enabled: boolean = true,
): UseDirectoryDetailsResult => {
  const { data, isLoading, error } = useQuery({
    queryKey: ["directorySize", path],
    queryFn: async () => {
      const response = await axios.get<DirectoryDetailsData>(
        "/navigator/api/dir-size",
        {
          params: { path },
          timeout: 60000, // 60 second timeout
        },
      );
      return response.data;
    },
    enabled: enabled && !!path,
    staleTime: CACHE_DURATION,
    gcTime: CACHE_PERSISTENCE,
    refetchOnWindowFocus: false,
    refetchOnMount: false,
    retry: (failureCount) => failureCount < MAX_RETRIES,
    retryDelay: () => FAILED_RETRY_DELAY,
  });

  const isUnavailable = error !== null && !data;

  return {
    size: data?.size ?? null,
    isLoading,
    error: error instanceof Error ? error : null,
    isUnavailable,
  };
};
