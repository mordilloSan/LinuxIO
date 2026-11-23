import { CACHE_TTL_MS, linuxio } from "@/api";
import { stripTrailingSlash } from "@/utils/path";

import { normalizeResource } from "../-fileUtils";
import { markDirectorySizesStale } from "./useListingInvalidation";

export const fileBrowserListingQueryOptions = {
  staleTime: CACHE_TTL_MS.TWO_SECONDS,
} as const;

export const createFileBrowserListingQueryOptions = (path: string) => {
  const normalizedPath = stripTrailingSlash(path || "/");
  const listing = linuxio.filebrowser.list_directory({ path: normalizedPath });
  const queryFn: typeof listing.queryFn = async (context) => {
    const data = await listing.queryFn(context);
    // The bridge has told the indexer to refresh the folder it just read.
    markDirectorySizesStale(context.client, normalizedPath);
    return data;
  };
  return {
    ...listing,
    queryFn,
    select: (data: Parameters<typeof normalizeResource>[0]) =>
      normalizeResource(data, normalizedPath),
    ...fileBrowserListingQueryOptions,
  };
};

export type FileBrowserListingQueryOptions = ReturnType<
  typeof createFileBrowserListingQueryOptions
>;
