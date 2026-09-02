import { useQueries } from "@tanstack/react-query";

import { linuxio } from "@/api";
import type { MultiStatsItem } from "@/types/filebrowser";
import { stripTrailingSlash } from "@/utils/path";

import {
  getDirectorySizeQueryOptions,
  shouldSkipSizeCalculation,
} from "./useFileDirectorySizeBase";

interface UseMultipleDirectoryDetailsResult {
  isAnyError: boolean;
  isAnyLoading: boolean;
  items: (MultiStatsItem & {
    isLoading: boolean;
    error: Error | null;
    aggregateSize?: number;
  })[];
  totalSize: number;
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

  // One dir_size query per directory - shares cache with useDirectorySize!
  const queries = useQueries({
    queries: directoryPaths.map((path) => ({
      ...linuxio.filebrowser.dir_size({ path: stripTrailingSlash(path) }),
      ...getDirectorySizeQueryOptions(),
      enabled: true,
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
      if (query) {
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
