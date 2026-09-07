import { CACHE_TTL_MS } from "@/api";

export const hardwareStableQueryOptions = {
  staleTime: CACHE_TTL_MS.ONE_DAY,
} as const;
