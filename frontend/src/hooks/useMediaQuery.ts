import { useCallback, useSyncExternalStore } from "react";

/* Tracks a CSS media query. Pair it with up()/down()/between() from
   theme/breakpoints so JS and the stylesheets read one viewport scale; the
   leading "@media" those helpers emit is accepted and stripped. Renders as
   `false` on the server and during hydration. */
export function useMediaQuery(query: string) {
  const normalizedQuery = query.trim().replace(/^@media\s*/i, "");

  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      if (typeof window === "undefined") return () => {};
      const mediaQueryList = window.matchMedia(normalizedQuery);
      mediaQueryList.addEventListener("change", onStoreChange);
      return () => mediaQueryList.removeEventListener("change", onStoreChange);
    },
    [normalizedQuery],
  );

  return useSyncExternalStore(
    subscribe,
    () =>
      typeof window !== "undefined"
        ? window.matchMedia(normalizedQuery).matches
        : false,
    () => false,
  );
}
