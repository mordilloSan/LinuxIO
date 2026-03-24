import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";

import { normalizeResource } from "@/components/filebrowser/utils";
import { useFileMultipleDirectoryDetails } from "@/hooks/useFileMultipleDirectoryDetails";
import { FileResource } from "@/types/filebrowser";
import {
  linuxio,
  CACHE_TTL_MS,
  LinuxIOError,
  useIsUpdating,
  useStreamMux,
} from "@/api";

interface useFileQueriesParams {
  normalizedPath: string;
  detailTarget: string[] | null;
  editingPath: string | null;
  hasSingleDetailTarget: boolean;
  hasMultipleDetailTargets: boolean;
}

export const useFileQueries = ({
  normalizedPath,
  detailTarget,
  editingPath,
  hasSingleDetailTarget,
  hasMultipleDetailTargets,
}: useFileQueriesParams) => {
  const { isOpen } = useStreamMux();
  const isUpdating = useIsUpdating();
  const {
    data: resourceData,
    isPending,
    isError,
    error,
  } = linuxio.filebrowser.resource_get.useQuery(normalizedPath, {
    staleTime: CACHE_TTL_MS.NONE,
  });

  const resource = useMemo(
    () => (resourceData ? normalizeResource(resourceData) : undefined),
    [resourceData],
  );

  const errorMessage = useMemo(() => {
    if (!isError || error === null || error === undefined) return null;

    const err = error as Error | LinuxIOError | null | undefined;
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

  // Detail resource query with content flag
  const {
    data: detailResource,
    isPending: isDetailPending,
    error: detailError,
  } = linuxio.filebrowser.resource_get.useQuery(
    detailTarget && detailTarget.length === 1 ? detailTarget[0] : "",
    "",
    "true",
    {
      enabled:
        hasSingleDetailTarget &&
        detailTarget !== null &&
        detailTarget.length === 1,
    },
  );

  const { data: statData, isPending: isStatPending } =
    linuxio.filebrowser.resource_stat.useQuery(
      detailTarget && detailTarget.length === 1 ? detailTarget[0] : "",
      {
        enabled:
          hasSingleDetailTarget &&
          detailTarget !== null &&
          detailTarget.length === 1,
      },
    );

  const multipleDetailTargets =
    hasMultipleDetailTargets && detailTarget !== null && detailTarget.length > 1
      ? detailTarget
      : [];
  const areMultipleResourcesEnabled =
    isOpen && !isUpdating && multipleDetailTargets.length > 1;

  const multipleResourceQueries = useQueries({
    queries: multipleDetailTargets.map((path) => ({
      ...linuxio.filebrowser.resource_get.queryOptions(path, {
        staleTime: CACHE_TTL_MS.NONE,
      }),
      enabled: areMultipleResourcesEnabled,
    })),
  });

  const multipleResourceData = multipleResourceQueries.map((q) => q.data);

  const multipleFileResources = useMemo(() => {
    if (multipleDetailTargets.length <= 1) return undefined;
    if (multipleResourceData.some((data) => data === undefined)) {
      return undefined;
    }

    return multipleDetailTargets.reduce(
      (acc, path, index) => {
        const queryData = multipleResourceData[index];
        if (queryData === undefined) {
          return acc;
        }
        acc[path] = normalizeResource(queryData);
        return acc;
      },
      {} as Record<string, FileResource>,
    );
  }, [multipleDetailTargets, ...multipleResourceData]);

  const isMultipleFilesPending =
    multipleDetailTargets.length > 1 &&
    multipleResourceQueries.some((query) => query.isPending);

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

  // Editing file resource with content flag
  const { data: editingFileResource, isPending: isEditingFileLoading } =
    linuxio.filebrowser.resource_get.useQuery(editingPath || "", "", "true", {
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
