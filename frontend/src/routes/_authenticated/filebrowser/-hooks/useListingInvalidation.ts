import { useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import { linuxio } from "@/api";
import { stripTrailingSlash } from "@/utils/path";

/**
 * Invalidation shared by every flow that changes a directory's contents
 * (file mutations, uploads, drag-and-drop): refresh the path's listing and
 * drop the cached subfolder sizes, which any change makes stale.
 */
export function useListingInvalidation(normalizedPath: string) {
  const queryClient = useQueryClient();

  return useCallback(() => {
    const path = stripTrailingSlash(normalizedPath || "/");
    const parentPath =
      path === "/" ? "/" : path.slice(0, path.lastIndexOf("/")) || "/";
    void queryClient.invalidateQueries({
      queryKey: linuxio.filebrowser.list_directory({ path }).queryKey,
    });
    for (const includeFiles of [false, true]) {
      void queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.directory_children({
          path,
          includeFiles,
        }).queryKey,
      });
    }
    void queryClient.invalidateQueries({
      queryKey: linuxio.filebrowser.dir_size({ path }).queryKey,
    });
    for (const aggregatePath of new Set([path, parentPath])) {
      void queryClient.invalidateQueries({
        queryKey: linuxio.filebrowser.subfolders({ path: aggregatePath })
          .queryKey,
      });
    }
  }, [normalizedPath, queryClient]);
}
