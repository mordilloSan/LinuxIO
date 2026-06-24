import { CACHE_TTL_MS, linuxio, type SearchResult } from "@/api";
import { useCapability } from "@/hooks/useCapabilities";

export type { SearchResult };

interface UseFileSearchOptions {
  basePath?: string;
  enabled?: boolean;
  limit?: number;
  query: string;
}

interface UseFileSearchResult {
  count: number;
  error: Error | null;
  isLoading: boolean;
  isUnavailable: boolean;
  results: SearchResult[];
}

export const useFileSearch = ({
  query,
  limit = 100,
  basePath = "/",
  enabled = true,
}: UseFileSearchOptions): UseFileSearchResult => {
  const {
    isEnabled: indexerEnabled,
    reason: indexerReason,
    status: indexerStatus,
  } = useCapability("indexerAvailable");
  const indexerDisabled = !indexerEnabled;
  const shouldSearch = query.trim().length >= 2; // Minimum 2 characters
  const queryEnabled = enabled && shouldSearch && !indexerDisabled;

  const { data, isLoading, error } = linuxio.filebrowser.search.useQuery(
    { query, limit: String(limit), basePath },
    {
      enabled: queryEnabled,
      staleTime: CACHE_TTL_MS.THIRTY_SECONDS, // Search results stay fresh longer
      gcTime: CACHE_TTL_MS.FIVE_MINUTES,
      refetchOnWindowFocus: false,
      refetchOnMount: false,
      retry: 1,
    },
  );

  const derivedError =
    indexerDisabled && shouldSearch
      ? new Error(
          indexerStatus === "unknown"
            ? indexerReason
            : "Search is unavailable (indexer offline)",
        )
      : error instanceof Error
        ? error
        : null;

  const isUnavailable = indexerDisabled && shouldSearch;

  return {
    results: data?.results ?? [],
    count: data?.count ?? 0,
    isLoading: queryEnabled ? isLoading : false,
    error: derivedError,
    isUnavailable,
  };
};
