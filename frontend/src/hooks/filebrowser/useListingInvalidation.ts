import { useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import { linuxio } from "@/api";

/**
 * Invalidation shared by every flow that changes a directory's contents
 * (file mutations, uploads, drag-and-drop): refresh the path's listing and
 * drop the cached subfolder sizes, which any change makes stale.
 */
export function useListingInvalidation(normalizedPath: string) {
  const queryClient = useQueryClient();

  return useCallback(() => {
    void queryClient.invalidateQueries({
      queryKey: linuxio.filebrowser.resource_get({ path: normalizedPath })
        .queryKey,
    });
    queryClient.removeQueries({
      queryKey: ["linuxio", "filebrowser", "subfolders"],
    });
  }, [normalizedPath, queryClient]);
}
