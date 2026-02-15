import { linuxio, CACHE_TTL_MS } from "@/api";

/**
 * Hook to fetch and cache a Docker icon by identifier
 * @param identifier Icon identifier (e.g., "si:nginx", "https://...", etc.)
 * @param enabled Whether to fetch the icon (default: true when identifier exists)
 * @returns Object with icon data URI and query state
 */
export function useDockerIcon(identifier: string | undefined, enabled = true) {
  const query = linuxio.docker.get_icon_uri.useQuery({
    args: identifier ? [identifier] : [],
    enabled: enabled && !!identifier,
    staleTime: CACHE_TTL_MS.ONE_DAY, // Icons rarely change
    gcTime: CACHE_TTL_MS.ONE_DAY, // Keep in cache for 24 hours
    retry: 1, // Only retry once for icons
  });

  return {
    iconUri: identifier ? query.data?.uri : null,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
  };
}
