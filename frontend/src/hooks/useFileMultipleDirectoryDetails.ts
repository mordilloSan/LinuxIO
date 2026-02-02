import { useQueries } from "@tanstack/react-query";

import { MultiStatsItem } from "@/types/filebrowser";
import linuxio from "@/api/react-query";
import {
  shouldSkipSizeCalculation,
  getDirectorySizeQueryOptions,
  useIndexerAvailability,
} from "./useFileDirectorySizeBase";

interface DirectoryDetailsData {
  path: string;
  size: number;
}

interface UseMultipleDirectoryDetailsResult {
  items: (MultiStatsItem & {
    isLoading: boolean;
    error: Error | null;
    aggregateSize?: number;
  })[];
  totalSize: number;
  isAnyError: boolean;
  isAnyLoading: boolean;
}

export const useFileMultipleDirectoryDetails = (
  paths: string[],
  fileResourceMap: Record<string, { name: string; type: string; size: number }>,
): UseMultipleDirectoryDetailsResult => {
  // Filter to only directories that should have size calculations
  const directoryPaths = paths.filter(
    (path) =>
      fileResourceMap[path]?.type === "directory" &&
      !shouldSkipSizeCalculation(path),
  );

  const indexerDisabled = useIndexerAvailability();
  const indexerUnavailableError = indexerDisabled
    ? new Error("Directory size indexing is unavailable")
    : null;

  // Use useQueries to fetch directory sizes - shares cache with useDirectorySize!
  const queries = useQueries({
    queries: directoryPaths.map((path) => ({
      queryKey: ["directorySize", path],
      queryFn: async () => {
        return linuxio.call<DirectoryDetailsData>("filebrowser", "dir_size", [
          path,
        ]);
      },
      ...getDirectorySizeQueryOptions(),
      enabled: !indexerDisabled,
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

    let isLoading = false;
    let aggregateSize: number | undefined;
    let itemError: Error | null = null;

    if (isDir) {
      if (indexerDisabled) {
        itemError = indexerUnavailableError;
      } else if (query) {
        isLoading = query.isLoading;
        if (isLoading) {
          result.isAnyLoading = true;
        }
        if (query.isError && query.error) {
          itemError = query.error;
        }
        aggregateSize = query.data?.size;
      }
    }

    if (itemError) {
      result.isAnyError = true;
    }

    result.items.push({
      path,
      name: fileInfo.name,
      type: fileInfo.type,
      size: fileInfo.size,
      isLoading,
      error: itemError,
      aggregateSize,
    });

    // For directories with fetched size, use that; otherwise use filesystem size
    if (isDir && aggregateSize !== undefined && !itemError) {
      result.totalSize += aggregateSize;
    } else {
      result.totalSize += fileInfo.size;
    }
  });

  return result;
};
