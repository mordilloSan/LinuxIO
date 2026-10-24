/** Cache scope used while nobody is signed in (and during auth bootstrap). */
export const ANONYMOUS_USER_ID = "anonymous";

/** The per-user scope under which user-owned query-cache entries live. */
export const cacheUserId = (user: { id: string } | null | undefined): string =>
  user?.id ?? ANONYMOUS_USER_ID;
