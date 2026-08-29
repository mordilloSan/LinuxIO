import { useQuery, useSuspenseQuery } from "@tanstack/react-query";

import { linuxio, type TextFile } from "@/api";
import type { FileBrowserListingQueryOptions } from "@/hooks/filebrowser/fileBrowserListingQueryOptions";
import { useFileMultipleDirectoryDetails } from "@/hooks/filebrowser/useFileMultipleDirectoryDetails";
import type { FileItem, FileResource } from "@/types/filebrowser";

interface UseFileQueriesParams {
  detailItems?: FileItem[];
  detailTarget: string[] | null;
  editingPath: string | null;
  hasMultipleDetailTargets: boolean;
  hasSingleDetailTarget: boolean;
  listingQueryOptions: FileBrowserListingQueryOptions;
}

const fileName = (path: string) =>
  path.split("/").filter(Boolean).pop() ?? path;

const textResource = (path: string, data: TextFile): FileResource => ({
  canOpenAsText: true,
  canSave: data.canSave,
  content: data.content,
  hidden: fileName(path).startsWith("."),
  isRegularFile: true,
  name: fileName(path),
  path,
  type: "file",
  version: data.version,
});

export const useFileQueries = ({
  detailItems,
  detailTarget,
  editingPath,
  hasSingleDetailTarget,
  hasMultipleDetailTargets,
  listingQueryOptions,
}: UseFileQueriesParams) => {
  const { data: resource } = useSuspenseQuery(listingQueryOptions);
  const selectedDetail = hasSingleDetailTarget
    ? detailItems?.find((item) => item.path === detailTarget?.[0])
    : undefined;
  const detailResource = selectedDetail;
  const detailError =
    hasSingleDetailTarget && detailTarget && !selectedDetail
      ? new Error("Selected item is no longer available")
      : null;

  const {
    data: statData,
    error: statError,
    isLoading: isStatLoading,
  } = useQuery({
    ...linuxio.filebrowser.resource_stat({
      path: selectedDetail?.path ?? "",
    }),
    enabled: hasSingleDetailTarget && !!selectedDetail,
  });

  const fileResourceMap: Record<
    string,
    { name: string; type: string; size: number }
  > = {};
  (detailItems ?? []).forEach((item) => {
    fileResourceMap[item.path] = {
      name: item.name,
      type: item.type,
      size: item.size ?? 0,
    };
  });
  const multiItemsStats = useFileMultipleDirectoryDetails(
    hasMultipleDetailTargets && detailTarget ? detailTarget : [],
    fileResourceMap,
  );

  const {
    data: editingData,
    error: editingFileError,
    isLoading: isEditingFileLoading,
  } = useQuery({
    ...linuxio.filebrowser.read_text({ path: editingPath || "" }),
    enabled: !!editingPath,
  });
  const editingFileResource =
    editingPath && editingData
      ? textResource(editingPath, editingData)
      : undefined;

  return {
    resource,
    detailResource,
    detailError: detailError ?? statError,
    statData,
    isStatLoading,
    multiItemsStats,
    editingFileResource,
    editingFileError,
    isEditingFileLoading,
    shouldShowDetailLoader: false,
  };
};
