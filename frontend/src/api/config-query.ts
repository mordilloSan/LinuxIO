import type { QueryKey } from "@tanstack/react-query";

/** Keep per-user configuration query entries isolated in the shared client. */
export function scopedConfigQueryKey(
  queryKey: QueryKey,
  userId: string,
): QueryKey {
  return [...queryKey, "user", userId];
}
