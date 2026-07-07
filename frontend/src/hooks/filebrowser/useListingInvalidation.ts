import { useCallback } from "react";

import { linuxio } from "@/api";

/**
 * Invalidation shared by every flow that changes a directory's contents
 * (file mutations, uploads, drag-and-drop): refresh the path's listing and
 * drop the cached subfolder sizes, which any change makes stale.
 */
export function useListingInvalidation(normalizedPath: string) {
  const resourceCache = linuxio.filebrowser.resource_get.useCache();
  const subfoldersCache = linuxio.filebrowser.subfolders.useCache();

  return useCallback(() => {
    void resourceCache.invalidate({ path: normalizedPath });
    subfoldersCache.remove();
  }, [normalizedPath, resourceCache, subfoldersCache]);
}
