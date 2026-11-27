import {
  type QueryFunctionContext,
  useQueries,
  useQuery,
  type UseQueryResult,
} from "@tanstack/react-query";

import {
  linuxio,
  type SubfoldersResponse,
  type TrashEntry,
  type TrashUsageResponse,
} from "@/api";
import useAuth from "@/hooks/useAuth";

import { trashFilesFolders } from "../-trashEntries";
import { getDirectorySizeQueryOptions } from "./useFileDirectorySizeBase";
import { markTrashSizesStale } from "./useListingInvalidation";

/**
 * The Trash's list. The bridge has told the indexer to refresh each trash it
 * read, so the sizes read from the index are marked stale as it arrives.
 */
export const trashListQueryOptions = () => {
  const list = linuxio.filebrowser.trash_list;
  return {
    ...list,
    queryFn: async (context: QueryFunctionContext) => {
      const data = await list.queryFn(context);
      markTrashSizesStale(context.client, trashFilesFolders(data.entries));
      return data;
    },
  };
};

const sizesByPath = (
  results: UseQueryResult<SubfoldersResponse>[],
): ReadonlyMap<string, number> => {
  const sizes = new Map<string, number>();
  for (const result of results) {
    for (const folder of result.data?.subfolders ?? []) {
      sizes.set(folder.path, folder.size);
    }
  }
  return sizes;
};

/**
 * The size of each trashed folder the index holds one for, by trashed path:
 * the sizes of the subfolders of each trash's `files` folder, one read per
 * trash, as a folder's listing sizes its folders. A session without sudo
 * cannot reach the index and gets none, as in a folder.
 */
export const useTrashFolderSizes = (
  entries: readonly TrashEntry[],
): ReadonlyMap<string, number> => {
  const { privileged } = useAuth();
  return useQueries({
    combine: sizesByPath,
    queries: trashFilesFolders(entries).map((path) => ({
      ...linuxio.filebrowser.subfolders({ path }),
      enabled: privileged,
      ...getDirectorySizeQueryOptions(),
    })),
  });
};

/**
 * What the session user's trashes take on disk, from the index; undefined
 * while it is not known and on a session without sudo. Every trash job marks
 * it stale, and so does reading the Trash's list.
 */
export const useTrashUsage = (): TrashUsageResponse | undefined => {
  const { privileged } = useAuth();
  return useQuery({
    ...linuxio.filebrowser.trash_usage,
    enabled: privileged,
    ...getDirectorySizeQueryOptions(),
  }).data;
};
