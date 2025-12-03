import { useQueries } from "@tanstack/react-query";
import axios from "@/utils/axios";
import { MultiStatsItem } from "@/types/filebrowser";

interface DirectoryDetailsData {
  path: string;
  size: number;
}

interface UseMultipleDirectoryDetailsResult {
  items: Array<
    MultiStatsItem & {
      isLoading: boolean;
      error: Error | null;
      aggregateSize?: number;
    }
  >;
  totalSize: number;
  isAnyError: boolean;
  isAnyLoading: boolean;
}

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes (same as useDirectorySize)
const CACHE_PERSISTENCE = 24 * 60 * 60 * 1000; // 24 hours
const FAILED_RETRY_DELAY = 30 * 1000;
const MAX_RETRIES = 2;

// Directories that should not have size calculations (not indexed by the indexer)
const EXCLUDED_DIRECTORIES = ["/proc", "/dev", "/sys"];

const shouldSkipSizeCalculation = (path: string): boolean => {
  if (!path) return true;
  return EXCLUDED_DIRECTORIES.some(
    (excluded) => path === excluded || path.startsWith(excluded + "/"),
  );
};

export const useMultipleDirectoryDetails = (
  paths: string[],
  fileResourceMap: Record<string, { name: string; type: string; size: number }>,
): UseMultipleDirectoryDetailsResult => {
  // Filter to only directories that should have size calculations
  const directoryPaths = paths.filter(
    (path) =>
      fileResourceMap[path]?.type === "directory" &&
      !shouldSkipSizeCalculation(path),
  );

  // Use useQueries to fetch directory sizes - shares cache with useDirectorySize!
  const queries = useQueries({
    queries: directoryPaths.map((path) => ({
      queryKey: ["directorySize", path],
      queryFn: async () => {
        const response = await axios.get<DirectoryDetailsData>(
          "/navigator/api/dir-size",
          {
            params: { path },
            timeout: 10000,
          },
        );
        return response.data;
      },
      staleTime: CACHE_DURATION,
      gcTime: CACHE_PERSISTENCE,
      refetchOnWindowFocus: false,
      refetchOnMount: false,
      retry: (failureCount: number) => failureCount < MAX_RETRIES,
      retryDelay: () => FAILED_RETRY_DELAY,
    })),
  });

  // Create a map of path -> query result for easy lookup
  const queryMap = new Map(
    directoryPaths.map((path, index) => [path, queries[index]]),
  );

  // Aggregate the results
  const result: UseMultipleDirectoryDetailsResult = {
    items: [],
    totalSize: 0,
    isAnyError: false,
    isAnyLoading: false,
  };

  paths.forEach((path) => {
    const fileInfo = fileResourceMap[path];
    if (!fileInfo) return;

    const isDir = fileInfo.type === "directory";
    const query = queryMap.get(path);

    // A directory is loading if we have a query for it and it's loading
    const isLoading = isDir && query ? query.isLoading : false;
    const hasError = isDir && query ? query.isError : false;
    const dirSize = query?.data?.size;

    if (isLoading) result.isAnyLoading = true;
    if (hasError) result.isAnyError = true;

    result.items.push({
      path,
      name: fileInfo.name,
      type: fileInfo.type,
      size: fileInfo.size,
      isLoading,
      error: hasError && query?.error ? (query.error as Error) : null,
      aggregateSize: isDir && dirSize !== undefined ? dirSize : undefined,
    });

    // For directories with fetched size, use that; otherwise use filesystem size
    if (isDir && dirSize !== undefined && !hasError) {
      result.totalSize += dirSize;
    } else {
      result.totalSize += fileInfo.size;
    }
  });

  return result;
};
