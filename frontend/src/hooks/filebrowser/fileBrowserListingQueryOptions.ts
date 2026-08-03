import { CACHE_TTL_MS } from "@/api";

export const fileBrowserListingQueryOptions = {
  staleTime: CACHE_TTL_MS.TWO_SECONDS,
} as const;
