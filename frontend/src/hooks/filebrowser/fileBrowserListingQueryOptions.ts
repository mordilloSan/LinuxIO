import { CACHE_TTL_MS, linuxio } from "@/api";
import { normalizeResource } from "@/components/filebrowser/utils";
import { stripTrailingSlash } from "@/utils/path";

export const fileBrowserListingQueryOptions = {
  staleTime: CACHE_TTL_MS.TWO_SECONDS,
} as const;

export const createFileBrowserListingQueryOptions = (path: string) => {
  const normalizedPath = stripTrailingSlash(path || "/");
  return {
    ...linuxio.filebrowser.list_directory({ path: normalizedPath }),
    select: (data: Parameters<typeof normalizeResource>[0]) =>
      normalizeResource(data, normalizedPath),
    ...fileBrowserListingQueryOptions,
  };
};

export type FileBrowserListingQueryOptions = ReturnType<
  typeof createFileBrowserListingQueryOptions
>;
