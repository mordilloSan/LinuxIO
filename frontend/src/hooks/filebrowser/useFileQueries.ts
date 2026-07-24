import { useQueries, useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { CACHE_TTL_MS, linuxio, LinuxIOError } from "@/api";
import { normalizeResource } from "@/components/filebrowser/utils";
import { useFileMultipleDirectoryDetails } from "@/hooks/filebrowser/useFileMultipleDirectoryDetails";

interface useFileQueriesParams {
  detailTarget: string[] | null;
  editingPath: string | null;
  hasMultipleDetailTargets: boolean;
  hasSingleDetailTarget: boolean;
  normalizedPath: string;
}

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
  } = useQuery(
    linuxio.filebrowser.resource_get.queryOptions(
      { path: normalizedPath },
      {
        staleTime: CACHE_TTL_MS.NONE,
      },
    ),
  );

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
    isLoading: isDetailLoading,
    error: detailError,
  } = useQuery(
    linuxio.filebrowser.resource_get.queryOptions(
      {
        path: detailTarget && detailTarget.length === 1 ? detailTarget[0] : "",
        unused: "",
        getContent: "true",
      },
      {
        enabled:
          hasSingleDetailTarget &&
          detailTarget !== null &&
          detailTarget.length === 1,
      },
    ),
  );

  const { data: statData, isLoading: isStatLoading } = useQuery(
    linuxio.filebrowser.resource_stat.queryOptions(
      detailTarget && detailTarget.length === 1 ? detailTarget[0] : "",
      {
        enabled:
          hasSingleDetailTarget &&
          detailTarget !== null &&
          detailTarget.length === 1,
      },
    ),
  );

  const multipleDetailTargets =
    hasMultipleDetailTargets && detailTarget !== null && detailTarget.length > 1
      ? detailTarget
      : [];

  const multipleResourceQueries = useQueries({
    queries: multipleDetailTargets.map((path) =>
      linuxio.filebrowser.resource_get.queryOptions(
        { path },
        {
          staleTime: CACHE_TTL_MS.NONE,
          enabled: multipleDetailTargets.length > 1,
        },
      ),
    ),
  });

  // Plain guarded derivation, not useMemo: memoizing would need a
  // variable-length dep array (hooks-contract violation) and the query
  // result array is fresh every render anyway. The map's identity churn is
  // safe downstream — useFileMultipleDirectoryDetails keys its queries by
  // path string, so it cannot cause refetch loops.
  const hasAllMultipleResources =
    multipleDetailTargets.length > 1 &&
    multipleResourceQueries.every((query) => query.data !== undefined);

  const fileResourceMap: Record<
    string,
    { name: string; type: string; size: number }
  > = {};
  if (hasAllMultipleResources) {
    multipleDetailTargets.forEach((path, index) => {
      const data = multipleResourceQueries[index]?.data;
      if (data === undefined) return;
      const resource = normalizeResource(data);
      fileResourceMap[path] = {
        name: resource.name,
        type: resource.type,
        size: resource.size ?? 0,
      };
    });
  }

  const isMultipleFilesLoading =
    multipleDetailTargets.length > 1 &&
    multipleResourceQueries.some((query) => query.isLoading);

  const multiItemsStats = useFileMultipleDirectoryDetails(
    detailTarget || [],
    fileResourceMap,
  );

  // Editing file resource with content flag
  const { data: editingFileResource, isLoading: isEditingFileLoading } =
    useQuery(
      linuxio.filebrowser.resource_get.queryOptions(
        { path: editingPath || "", unused: "", getContent: "true" },
        {
          enabled: !!editingPath,
        },
      ),
    );

  const shouldShowDetailLoader =
    (hasSingleDetailTarget && isDetailLoading) ||
    (hasMultipleDetailTargets && isMultipleFilesLoading);

  return {
    resource,
    isPending,
    errorMessage,
    detailResource,
    detailError,
    statData,
    isStatLoading,
    multiItemsStats,
    editingFileResource,
    isEditingFileLoading,
    shouldShowDetailLoader,
  };
};
