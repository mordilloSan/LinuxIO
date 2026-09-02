import { useQuery } from "@tanstack/react-query";

import { CACHE_TTL_MS, linuxio, type SearchResult } from "@/api";

interface UseFileSearchOptions {
  basePath?: string;
  caseSensitive?: boolean;
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

export const MIN_SEARCH_QUERY_LENGTH = 3;

export const isSearchableQuery = (query: string) =>
  Array.from(query.trim()).length >= MIN_SEARCH_QUERY_LENGTH;

export const useFileSearch = ({
  query,
  limit = 100,
  basePath = "/",
  caseSensitive = false,
  enabled = true,
}: UseFileSearchOptions): UseFileSearchResult => {
  const shouldSearch = isSearchableQuery(query);
  const queryEnabled = enabled && shouldSearch;
  const backendQuery = caseSensitive ? `case:exact ${query}` : query;

  const { data, isLoading, error } = useQuery({
    ...linuxio.filebrowser.search({
      query: backendQuery,
      limit: String(limit),
      basePath,
    }),
    ...{
      enabled: queryEnabled,
      staleTime: CACHE_TTL_MS.THIRTY_SECONDS, // Search results stay fresh longer
      gcTime: CACHE_TTL_MS.FIVE_MINUTES,
      refetchOnWindowFocus: false,
      refetchOnMount: false,
      retry: 1,
    },
  });

  const derivedError = error instanceof Error ? error : null;

  const isUnavailable = Boolean(derivedError) && shouldSearch;

  return {
    results: data?.results ?? [],
    count: data?.results.length ?? 0,
    isLoading: queryEnabled ? isLoading : false,
    error: derivedError,
    isUnavailable,
  };
};
