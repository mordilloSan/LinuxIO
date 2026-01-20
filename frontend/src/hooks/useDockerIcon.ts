import { useQuery } from "@tanstack/react-query";
import { useCallback } from "react";

import linuxio from "@/api/react-query";

/**
 * Hook to fetch and cache a Docker icon by identifier
 * @param identifier Icon identifier (e.g., "si:nginx", "https://...", etc.)
 * @param enabled Whether to fetch the icon (default: true when identifier exists)
 * @returns Object with icon data URI and query state
 */
export function useDockerIcon(identifier: string | undefined, enabled = true) {
  const query = useQuery({
    queryKey: ["docker", "icon", identifier],
    queryFn: async () => {
      if (!identifier) return null;

      const result = await linuxio.call<{ uri: string }>(
        "docker",
        "get_icon_uri",
        [identifier],
      );
      return result.uri;
    },
    enabled: enabled && !!identifier,
    staleTime: 24 * 60 * 60 * 1000, // 24 hours - icons rarely change
    gcTime: 24 * 60 * 60 * 1000, // Keep in cache for 24 hours
    retry: 1, // Only retry once for icons
  });

  return {
    iconUri: query.data,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
  };
}

/**
 * Hook to prefetch multiple icon identifiers
 * Useful for prefetching icons for a list of items
 */
export function usePrefetchDockerIcons() {
  const prefetch = useCallback(async (identifiers: (string | undefined)[]) => {
    const validIdentifiers = identifiers.filter(Boolean) as string[];

    // Prefetch all icons in parallel
    const promises = validIdentifiers.map((identifier) =>
      linuxio.call<{ uri: string }>("docker", "get_icon_uri", [identifier]),
    );

    try {
      await Promise.allSettled(promises);
    } catch (error) {
      // Silently fail - prefetch is best-effort
      console.debug("Failed to prefetch some icons:", error);
    }
  }, []);

  return { prefetch };
}

/**
 * Simple component-level cache for icons
 * Use this if you don't want to use React Query
 */
const iconCache = new Map<string, string>();

export async function getDockerIcon(
  identifier: string,
): Promise<string | null> {
  if (iconCache.has(identifier)) {
    return iconCache.get(identifier)!;
  }

  try {
    const result = await linuxio.call<{ uri: string }>(
      "docker",
      "get_icon_uri",
      [identifier],
    );
    iconCache.set(identifier, result.uri);
    return result.uri;
  } catch (error) {
    console.error(`Failed to fetch icon for ${identifier}:`, error);
    return null;
  }
}
