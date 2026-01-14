import linuxio from "@/api/react-query";
import useAuth from "@/hooks/useAuth";

export interface SearchResult {
  path: string;
  name: string;
  size: number;
  type?: string;
  isDir?: boolean;
  modTime?: string;
}

export interface SearchResponse {
  query: string;
  results: SearchResult[];
  count: number;
}

interface UseFileSearchOptions {
  query: string;
  limit?: number;
  basePath?: string;
  enabled?: boolean;
}

interface UseFileSearchResult {
  results: SearchResult[];
  count: number;
  isLoading: boolean;
  error: Error | null;
  isUnavailable: boolean;
}

export const useFileSearch = ({
  query,
  limit = 100,
  basePath = "/",
  enabled = true,
}: UseFileSearchOptions): UseFileSearchResult => {
  const { indexerAvailable } = useAuth();
  const indexerDisabled = indexerAvailable === false;
  const shouldSearch = query.trim().length >= 2; // Minimum 2 characters
  const queryEnabled = enabled && shouldSearch && !indexerDisabled;

  const { data, isLoading, error } = linuxio.filebrowser.search.useQuery(
    query,
    String(limit),
    basePath,
    {
      enabled: queryEnabled,
      staleTime: 30000, // 30 seconds - search results stay fresh longer
      gcTime: 5 * 60 * 1000, // 5 minutes
      refetchOnWindowFocus: false,
      refetchOnMount: false,
      retry: 1,
    },
  );

  const derivedError =
    indexerDisabled && shouldSearch
      ? new Error("Search is unavailable (indexer offline)")
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
