import { CACHE_TTL_MS } from "@/api";

export const hardwareSensorQueryOptions = {
  staleTime: CACHE_TTL_MS.TWO_SECONDS,
} as const;

export const hardwareStableQueryOptions = {
  staleTime: CACHE_TTL_MS.FIVE_MINUTES,
} as const;

export const hardwareGpuQueryOptions = {
  staleTime: CACHE_TTL_MS.ONE_MINUTE,
} as const;
