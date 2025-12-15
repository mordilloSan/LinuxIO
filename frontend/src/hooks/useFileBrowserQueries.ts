import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { normalizeResource } from "@/components/filebrowser/utils";
import { useMultipleDirectoryDetails } from "@/hooks/useMultipleDirectoryDetails";
import {
  ApiResource,
  FileResource,
  ResourceStatData,
} from "@/types/filebrowser";
import axios from "@/utils/axios";

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
  const {
    data: resource,
    isPending,
    isError,
    error,
  } = useQuery<FileResource>({
    queryKey: ["fileResource", normalizedPath],
    queryFn: async () => {
      const { data } = await axios.get<ApiResource>(
        "/navigator/api/resources",
        {
          params: {
            path: normalizedPath,
            source: "/",
          },
        },
      );
      return normalizeResource(data);
    },
    staleTime: 0,
  });

  const errorMessage = useMemo(() => {
    if (!isError) return null;
    if (error instanceof Error) {
      const axiosError = error as any;
      if (axiosError.response?.status === 403) {
        return `Permission denied: You don't have access to "${normalizedPath}".`;
      }
      if (
        axiosError.response?.status === 404 ||
        axiosError.response?.status === 500
      ) {
        return `Path not found: "${normalizedPath}" does not exist.`;
      }
      return error.message;
    }
    return "Failed to load file information.";
  }, [error, isError, normalizedPath]);

  const {
    data: detailResource,
    isPending: isDetailPending,
    error: detailError,
  } = useQuery<FileResource>({
    queryKey: ["fileDetail", detailTarget],
    queryFn: async () => {
      const currentDetailTarget = detailTarget;
      if (!currentDetailTarget || currentDetailTarget.length !== 1) {
        throw new Error("Invalid selection");
      }
      const { data } = await axios.get<ApiResource>(
        "/navigator/api/resources",
        {
          params: { path: currentDetailTarget[0], content: "true" },
        },
      );
      return data as FileResource;
    },
    enabled: hasSingleDetailTarget,
  });

  const { data: statData, isPending: isStatPending } =
    useQuery<ResourceStatData>({
      queryKey: ["fileStat", detailTarget],
      queryFn: async () => {
        const currentDetailTarget = detailTarget;
        if (!currentDetailTarget || currentDetailTarget.length !== 1) {
          throw new Error("Invalid selection");
        }
        const { data } = await axios.get<ResourceStatData>(
          "/navigator/api/resources/stat",
          {
            params: { path: currentDetailTarget[0] },
          },
        );
        return data;
      },
      enabled: hasSingleDetailTarget,
    });

  const { data: multipleFileResources, isPending: isMultipleFilesPending } =
    useQuery<Record<string, FileResource>>({
      queryKey: ["multipleFileDetails", detailTarget],
      queryFn: async () => {
        const currentDetailTarget = detailTarget;
        if (!currentDetailTarget || currentDetailTarget.length <= 1) {
          throw new Error("Invalid selection");
        }
        const results: Record<string, FileResource> = {};
        await Promise.all(
          currentDetailTarget.map(async (path) => {
            const { data } = await axios.get<ApiResource>(
              "/navigator/api/resources",
              {
                params: { path },
              },
            );
            results[path] = normalizeResource(data);
          }),
        );
        return results;
      },
      enabled: hasMultipleDetailTargets,
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
      queryKey: ["fileEdit", editingPath],
      queryFn: async () => {
        if (!editingPath) throw new Error("No editing path");
        const { data } = await axios.get<ApiResource>(
          "/navigator/api/resources",
          {
            params: { path: editingPath, content: "true" },
          },
        );
        return data as FileResource;
      },
      enabled: !!editingPath,
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
