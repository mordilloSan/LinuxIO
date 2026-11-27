import type { QueryClient } from "@tanstack/react-query";

import { linuxio, type DirectoryListing, type SubfoldersResponse } from "@/api";
import { baseName, parentPath, stripTrailingSlash } from "@/utils/path";

/**
 * What the items at `paths` take on disk as far as the page already knows,
 * or undefined when it does not know for one of them: a folder's size is the
 * one the index gave for its folder's subfolders, a file's the one its
 * folder's listing gave. A size the index is still catching up on (`pending`)
 * or that the page has marked stale is not known: it may be the size from
 * before a copy into the folder. It reads the queries the page holds and asks
 * for nothing, so it is called where something happens (a delete is asked for, a
 * move to Trash starts) and not while rendering.
 */
export const knownDiskSize = (
  queryClient: QueryClient,
  paths: readonly string[],
): number | undefined => {
  let total = 0;
  for (const item of paths) {
    const path = stripTrailingSlash(item);
    const folder = parentPath(path);
    const sizes = queryClient.getQueryState<SubfoldersResponse>(
      linuxio.filebrowser.subfolders({ path: folder }).queryKey,
    );
    const sized = sizes?.data?.subfolders.find(
      (subfolder) => subfolder.path === path,
    );
    if (sized) {
      if (sizes?.data?.pending || sizes?.isInvalidated) return undefined;
      total += sized.size;
      continue;
    }
    const name = baseName(path);
    const file = queryClient
      .getQueryData<DirectoryListing>(
        linuxio.filebrowser.list_directory({ path: folder }).queryKey,
      )
      ?.files?.find((listed) => listed.name === name);
    if (!file) return undefined;
    total += file.size;
  }
  return total;
};
