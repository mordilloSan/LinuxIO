import { useQuery } from "@tanstack/react-query";
import axios from "@/utils/axios";
import { MultiStatsItem } from "@/types/filebrowser";

interface DirectoryDetailsData {
  path: string;
  size: number;
}

interface DirectoryDetailsWithError extends DirectoryDetailsData {
  error?: Error | null;
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

const CACHE_DURATION = 1 * 1 * 1000; // 30 seconds
const CACHE_PERSISTENCE = 24 * 60 * 60 * 1000; // 24 hours

export const useMultipleDirectoryDetails = (
  paths: string[],
  fileResourceMap: Record<string, { name: string; type: string; size: number }>,
): UseMultipleDirectoryDetailsResult => {
  // Fetch directory sizes for all directories
  const { data: directoryDetailsMap = {}, isLoading: isQueryLoading } =
    useQuery({
      queryKey: ["multipleDirectorySizes", paths.sort()],
      queryFn: async () => {
        const directoryPaths = paths.filter(
          (path) => fileResourceMap[path]?.type === "directory",
        );

        if (directoryPaths.length === 0) {
          return {};
        }

        const results: Record<string, DirectoryDetailsWithError> = {};

        await Promise.all(
          directoryPaths.map(async (path) => {
            try {
              const response = await axios.get<DirectoryDetailsData>(
                "/navigator/api/dir-size",
                {
                  params: { path },
                  timeout: 60000,
                },
              );
              results[path] = response.data;
            } catch (error) {
              results[path] = {
                path,
                size: 0,
                error:
                  error instanceof Error
                    ? error
                    : new Error("Failed to load directory size"),
              };
            }
          }),
        );

        return results;
      },
      enabled: paths.length > 0 && Object.keys(fileResourceMap).length > 0,
      staleTime: CACHE_DURATION,
      gcTime: CACHE_PERSISTENCE,
      refetchOnWindowFocus: false,
      refetchOnMount: false,
    });

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
    const dirDetails = directoryDetailsMap[path] as
      | DirectoryDetailsWithError
      | undefined;

    // A directory is loading if the query is loading AND we don't have details for it yet
    const isLoading = isDir && isQueryLoading && !dirDetails;
    // Has error if it's a directory and we have an error for it
    const hasError =
      isDir && dirDetails?.error !== undefined && dirDetails?.error !== null;

    if (isLoading) result.isAnyLoading = true;
    if (hasError) result.isAnyError = true;

    result.items.push({
      path,
      name: fileInfo.name,
      type: fileInfo.type,
      size: fileInfo.size,
      isLoading,
      error: hasError ? dirDetails?.error || null : null,
      aggregateSize:
        isDir && dirDetails && !dirDetails.error ? dirDetails.size : undefined,
    });

    // For directories, use aggregate size from dir-size; for files, use filesystem size
    if (isDir && dirDetails && !dirDetails.error) {
      result.totalSize += dirDetails.size;
    } else {
      result.totalSize += fileInfo.size;
    }
  });

  return result;
};
