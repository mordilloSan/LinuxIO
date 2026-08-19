import { CACHE_TTL_MS, linuxio } from "@/api";

export const fileBrowserListingQueryOptions = {
  staleTime: CACHE_TTL_MS.TWO_SECONDS,
} as const;

export const createFileBrowserListingQueryOptions = (path: string) => ({
  ...linuxio.filebrowser.resource_get({ path }),
  ...fileBrowserListingQueryOptions,
});

export type FileBrowserListingQueryOptions = ReturnType<
  typeof createFileBrowserListingQueryOptions
>;
