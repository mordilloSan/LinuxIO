import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { normalizeResource } from "@/components/filebrowser/utils";
import { useFileMultipleDirectoryDetails } from "@/hooks/useFileMultipleDirectoryDetails";
import {
  ApiResource,
  FileResource,
  ResourceStatData,
} from "@/types/filebrowser";
import linuxio, { LinuxIOError } from "@/api/react-query";

type useFileQueriesParams = {
  normalizedPath: string;
  detailTarget: string[] | null;
  editingPath: string | null;
  hasSingleDetailTarget: boolean;
  hasMultipleDetailTargets: boolean;
};

export const useFileQueries = ({
  normalizedPath,
  detailTarget,
  editingPath,
  hasSingleDetailTarget,
  hasMultipleDetailTargets,
}: useFileQueriesParams) => {
  const {
    data: resourceData,
    isPending,
    isError,
    error,
  } = linuxio.useCall<ApiResource>(
    "filebrowser",
    "resource_get",
    [normalizedPath],
    {
      staleTime: 0,
    },
  );

  const resource = useMemo(
    () => (resourceData ? normalizeResource(resourceData) : undefined),
    [resourceData],
  );

  const errorMessage = useMemo(() => {
    if (!isError || error === null || error === undefined) return null;

    const err = error as Error | LinuxIOError;
    if (err instanceof LinuxIOError) {
      if (err.code === 403) {
        return `Permission denied: You don't have access to "${normalizedPath}".`;
      }
      if (err.code === 404 || err.code === 500) {
        return `Path not found: "${normalizedPath}" does not exist.`;
      }
      return err.message;
    }
    if (err instanceof Error) {
      return err.message;
    }
    return "Failed to load file information.";
  }, [error, isError, normalizedPath]);

  const {
    data: detailResource,
    isPending: isDetailPending,
    error: detailError,
  } = linuxio.useCall<FileResource>(
    "filebrowser",
    "resource_get",
    detailTarget && detailTarget.length === 1
      ? [detailTarget[0], "", "true"]
      : [],
    {
      enabled:
        hasSingleDetailTarget &&
        detailTarget !== null &&
        detailTarget.length === 1,
    },
  );

  const { data: statData, isPending: isStatPending } =
    linuxio.useCall<ResourceStatData>(
      "filebrowser",
      "resource_stat",
      detailTarget && detailTarget.length === 1 ? [detailTarget[0]] : [],
      {
        enabled:
          hasSingleDetailTarget &&
          detailTarget !== null &&
          detailTarget.length === 1,
      },
    );

  const { data: multipleFileResources, isPending: isMultipleFilesPending } =
    useQuery<Record<string, FileResource>>({
      queryKey: ["linuxio", "filebrowser", "resource_get_multi", detailTarget],
      queryFn: async () => {
        const currentDetailTarget = detailTarget;
        if (!currentDetailTarget || currentDetailTarget.length <= 1) {
          throw new Error("Invalid selection");
        }
        const results: Record<string, FileResource> = {};
        await Promise.all(
          currentDetailTarget.map(async (path) => {
            // Args: [path]
            const data = await linuxio.call<ApiResource>(
              "filebrowser",
              "resource_get",
              [path],
            );
            results[path] = normalizeResource(data);
          }),
        );
        return results;
      },
      enabled:
        hasMultipleDetailTargets &&
        detailTarget !== null &&
        detailTarget.length > 1,
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

  const multiItemsStats = useFileMultipleDirectoryDetails(
    detailTarget || [],
    fileResourceMap,
  );

  const { data: editingFileResource, isPending: isEditingFileLoading } =
    linuxio.useCall<FileResource>(
      "filebrowser",
      "resource_get",
      editingPath ? [editingPath, "", "true"] : [],
      {
        enabled: !!editingPath,
      },
    );

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
