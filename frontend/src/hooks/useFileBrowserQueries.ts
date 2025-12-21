import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { normalizeResource } from "@/components/filebrowser/utils";
import { useMultipleDirectoryDetails } from "@/hooks/useMultipleDirectoryDetails";
import { useStreamMux } from "@/hooks/useStreamMux";
import {
  ApiResource,
  FileResource,
  ResourceStatData,
} from "@/types/filebrowser";
import { streamApi, StreamApiError } from "@/utils/streamApi";

type UseFileBrowserQueriesParams = {
  normalizedPath: string;
  detailTarget: string[] | null;
  editingPath: string | null;
  hasSingleDetailTarget: boolean;
  hasMultipleDetailTargets: boolean;
};

export const useFileBrowserQueries = ({
  normalizedPath,
  detailTarget,
  editingPath,
  hasSingleDetailTarget,
  hasMultipleDetailTargets,
}: UseFileBrowserQueriesParams) => {
  const { isOpen } = useStreamMux();

  const {
    data: resource,
    isPending,
    isError,
    error,
  } = useQuery<FileResource>({
    queryKey: ["stream", "filebrowser", "resource_get", normalizedPath],
    queryFn: async () => {
      // Args: [path]
      const data = await streamApi.get<ApiResource>(
        "filebrowser",
        "resource_get",
        [normalizedPath],
      );
      return normalizeResource(data);
    },
    staleTime: 0,
    enabled: isOpen,
  });

  const errorMessage = useMemo(() => {
    if (!isError) return null;
    if (error instanceof StreamApiError) {
      if (error.code === 403) {
        return `Permission denied: You don't have access to "${normalizedPath}".`;
      }
      if (error.code === 404 || error.code === 500) {
        return `Path not found: "${normalizedPath}" does not exist.`;
      }
      return error.message;
    }
    if (error instanceof Error) {
      return error.message;
    }
    return "Failed to load file information.";
  }, [error, isError, normalizedPath]);

  const {
    data: detailResource,
    isPending: isDetailPending,
    error: detailError,
  } = useQuery<FileResource>({
    queryKey: ["stream", "filebrowser", "resource_get_detail", detailTarget],
    queryFn: async () => {
      const currentDetailTarget = detailTarget;
      if (!currentDetailTarget || currentDetailTarget.length !== 1) {
        throw new Error("Invalid selection");
      }
      // Args: [path, "", getContent?]
      const data = await streamApi.get<ApiResource>(
        "filebrowser",
        "resource_get",
        [currentDetailTarget[0], "", "true"],
      );
      return data as FileResource;
    },
    enabled: isOpen && hasSingleDetailTarget,
  });

  const { data: statData, isPending: isStatPending } =
    useQuery<ResourceStatData>({
      queryKey: ["stream", "filebrowser", "resource_stat", detailTarget],
      queryFn: async () => {
        const currentDetailTarget = detailTarget;
        if (!currentDetailTarget || currentDetailTarget.length !== 1) {
          throw new Error("Invalid selection");
        }
        // Args: [path]
        const data = await streamApi.get<ResourceStatData>(
          "filebrowser",
          "resource_stat",
          [currentDetailTarget[0]],
        );
        return data;
      },
      enabled: isOpen && hasSingleDetailTarget,
    });

  const { data: multipleFileResources, isPending: isMultipleFilesPending } =
    useQuery<Record<string, FileResource>>({
      queryKey: ["stream", "filebrowser", "resource_get_multi", detailTarget],
      queryFn: async () => {
        const currentDetailTarget = detailTarget;
        if (!currentDetailTarget || currentDetailTarget.length <= 1) {
          throw new Error("Invalid selection");
        }
        const results: Record<string, FileResource> = {};
        await Promise.all(
          currentDetailTarget.map(async (path) => {
            // Args: [path]
            const data = await streamApi.get<ApiResource>(
              "filebrowser",
              "resource_get",
              [path],
            );
            results[path] = normalizeResource(data);
          }),
        );
        return results;
      },
      enabled: isOpen && hasMultipleDetailTargets,
    });

  const fileResourceMap = useMemo(() => {
    if (!multipleFileResources) return {};
    return Object.entries(multipleFileResources).reduce(
      (acc, [path, resource]) => {
        acc[path] = {
          name: resource.name,
          type: resource.type,
          size: resource.size ?? 0,
        };
        return acc;
      },
      {} as Record<string, { name: string; type: string; size: number }>,
    );
  }, [multipleFileResources]);

  const multiItemsStats = useMultipleDirectoryDetails(
    detailTarget || [],
    fileResourceMap,
  );

  const { data: editingFileResource, isPending: isEditingFileLoading } =
    useQuery<FileResource>({
      queryKey: ["stream", "filebrowser", "resource_get_edit", editingPath],
      queryFn: async () => {
        if (!editingPath) throw new Error("No editing path");
        // Args: [path, "", getContent?]
        const data = await streamApi.get<ApiResource>(
          "filebrowser",
          "resource_get",
          [editingPath, "", "true"],
        );
        return data as FileResource;
      },
      enabled: isOpen && !!editingPath,
    });

  const shouldShowDetailLoader =
    (hasSingleDetailTarget && isDetailPending) ||
    (hasMultipleDetailTargets && isMultipleFilesPending);

  return {
    resource,
    isPending,
    errorMessage,
    detailResource,
    isDetailPending,
    detailError,
    statData,
    isStatPending,
    multiItemsStats,
    editingFileResource,
    isEditingFileLoading,
    shouldShowDetailLoader,
  };
};
