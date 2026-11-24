import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import { linuxio } from "@/api";
import { stripTrailingSlash } from "@/utils/path";

/**
 * The sizes of a folder are kept for minutes, the listing for seconds. When
 * the bridge reads a folder it also tells the indexer to bring that folder's
 * rows up to date, so the sizes held from before the read may be old: the
 * listing's query calls this when its answer arrives, and they are asked for
 * again whenever the folder itself was. An answer that comes before the
 * indexer is done says so, and the size queries ask once more.
 */
export function markDirectorySizesStale(
  queryClient: QueryClient,
  normalizedPath: string,
) {
  const path = stripTrailingSlash(normalizedPath || "/");
  void queryClient.invalidateQueries({
    queryKey: linuxio.filebrowser.subfolders({ path }).queryKey,
  });
  void queryClient.invalidateQueries({
    queryKey: linuxio.filebrowser.dir_size({ path }).queryKey,
  });
}

/**
 * The folder's watch saw its entries change, or missed a while: mark its
 * listing stale. Only the listing, since its own query marks the folder's
 * sizes stale when its answer arrives. With `fetch` false the listing is
 * marked and not fetched (a hidden tab); the watch asks again when the tab
 * shows. A read already running is not restarted, so a folder whose listing
 * takes longer than the watch's frames still updates; it may have started
 * before the change, so the listing is read once more when it settles.
 */
export function markListingStale(
  queryClient: QueryClient,
  normalizedPath: string,
  fetch: boolean,
) {
  const path = stripTrailingSlash(normalizedPath || "/");
  const queryKey = linuxio.filebrowser.list_directory({ path }).queryKey;
  const running =
    queryClient.getQueryState(queryKey)?.fetchStatus === "fetching";
  const marked = queryClient.invalidateQueries(
    { queryKey, refetchType: fetch ? "active" : "none" },
    { cancelRefetch: false },
  );
  if (!running || !fetch) return;
  void marked.then(() =>
    queryClient.invalidateQueries(
      { queryKey, refetchType: "active" },
      { cancelRefetch: false },
    ),
  );
}

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
