import { useEffect, useRef, useState, useCallback, useMemo } from "react";

interface UseStreamingFetchOptions {
  params?: Record<string, string | number>;
  onChunk?: (chunk: any) => void;
  headers?: Record<string, string>;
}

interface UseStreamingFetchState<T> {
  data: T | null;
  isLoading: boolean;
  isRefetching: boolean;
  error: Error | null;
  progress: number;
}

/**
 * Hook for streaming NDJSON responses from API endpoints.
 *
 * Parses newline-delimited JSON and progressively updates state as chunks arrive.
 * Perfect for large directory listings or streaming data.
 *
 * Maintains previous data during refetches (folder navigation) to avoid flickering.
 *
 * @template T - The expected data type
 * @param url - The API endpoint URL
 * @param options - Fetch options with params support
 * @returns Object with data, isLoading, isRefetching, error, and progress state
 *
 * @example
 * const { data, isLoading, error } = useStreamingFetch<FileResource>(
 *   "/navigator/api/resources",
 *   { params: { path: "/bin", source: "/" } }
 * );
 */
export function useStreamingFetch<T>(
  url: string,
  options: UseStreamingFetchOptions = {},
): UseStreamingFetchState<T> {
  const [data, setData] = useState<T | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isRefetching, setIsRefetching] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [progress, setProgress] = useState(0);
  const [hasData, setHasData] = useState(false);

  const abortControllerRef = useRef<AbortController | null>(null);
  const urlRef = useRef(url);
  const optionsRef = useRef(options);

  const parseChunk = useCallback(
    (line: string): any => {
      if (!line.trim()) {
        return null;
      }

      try {
        return JSON.parse(line);
      } catch (e) {
        console.warn("Failed to parse chunk:", line, e);
        return null;
      }
    },
    [],
  );

  // Create a cache key that includes both URL and params
  const cacheKey = useMemo(() => {
    let key = url;
    if (options?.params) {
      const params = new URLSearchParams();
      Object.entries(options.params)
        .sort(([a], [b]) => a.localeCompare(b))
        .forEach(([k, v]) => {
          params.append(k, String(v));
        });
      key = `${url}?${params.toString()}`;
    }
    return key;
  }, [url, options?.params]);

  useEffect(() => {
    urlRef.current = url;
    optionsRef.current = options;
  }, [url, options]);

  useEffect(() => {
    const controller = new AbortController();
    abortControllerRef.current = controller;
    const signal = controller.signal;

    const fetchStream = async () => {
      try {
        // If we already have data, this is a refetch (e.g., folder navigation)
        // Otherwise, this is initial load
        if (hasData) {
          setIsRefetching(true);
        } else {
          setIsLoading(true);
        }
        setError(null);
        setProgress(0);

        // Build URL with query params
        let fetchUrl = url;
        if (optionsRef.current.params) {
          const params = new URLSearchParams();
          Object.entries(optionsRef.current.params).forEach(([key, value]) => {
            params.append(key, String(value));
          });
          fetchUrl = `${url}?${params.toString()}`;
        }

        const response = await fetch(fetchUrl, {
          signal,
          headers: {
            Accept: "application/x-ndjson",
            ...optionsRef.current.headers,
          },
        });

        if (!response.ok) {
          throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }

        const reader = response.body?.getReader();
        if (!reader) {
          throw new Error("Response body is not readable");
        }

        const decoder = new TextDecoder();
        let buffer = "";
        let chunkCount = 0;

        while (true) {
          const { done, value } = await reader.read();

          if (done) {
            // Process any remaining content in buffer
            if (buffer.trim()) {
              const chunk = parseChunk(buffer);
              if (chunk) {
                setData(chunk as T);
                setHasData(true);
                chunkCount++;
              }
            }
            break;
          }

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");

          // Process all complete lines
          for (let i = 0; i < lines.length - 1; i++) {
            const line = lines[i];
            const chunk = parseChunk(line);
            if (chunk) {
              // If this is a partial update (item), merge it with existing data
              // Otherwise, replace data entirely
              setData((prevData) => {
                const newData = (() => {
                  if (!prevData) {
                    return chunk as T;
                  }

                  // If chunk has an "items" array, merge files/folders
                  if (
                    typeof chunk === "object" &&
                    chunk.items &&
                    Array.isArray(chunk.items)
                  ) {
                    return {
                      ...prevData,
                      ...chunk,
                      items: [
                        ...(prevData as any)?.items || [],
                        ...chunk.items,
                      ],
                    };
                  }

                  // Otherwise just replace
                  return chunk as T;
                })();

                // Mark that we have data on first chunk
                if (!hasData) {
                  setHasData(true);
                }

                return newData;
              });

              chunkCount++;
              setProgress(chunkCount);

              if (optionsRef.current.onChunk) {
                optionsRef.current.onChunk(chunk);
              }
            }
          }

          // Keep the last incomplete line in buffer
          buffer = lines[lines.length - 1];
        }

        // Clear loading/refetching states
        setIsLoading(false);
        setIsRefetching(false);
      } catch (err) {
        if (err instanceof Error && err.name !== "AbortError") {
          setError(err);
        }
        setIsLoading(false);
        setIsRefetching(false);
      }
    };

    fetchStream();

    return () => {
      controller.abort();
    };
  }, [cacheKey, hasData]);

  return { data, isLoading, isRefetching, error, progress };
}
