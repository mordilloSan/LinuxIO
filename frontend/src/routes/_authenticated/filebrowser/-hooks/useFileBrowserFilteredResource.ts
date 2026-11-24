import { useMemo } from "react";

import useAuth from "@/hooks/useAuth";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";

import type { FileResource } from "../-types";
import { isSearchableQuery, useFileSearch } from "./useFileSearch";

interface UseFileBrowserFilteredResourceParams {
  /** The folder the page shows: a search finds what is below it. */
  basePath: string;
  caseSensitive?: boolean;
  resource?: FileResource;
  searchQuery: string;
}

// How long the typed query and the folder stay unchanged before they are
// searched: a search inside a folder reads it on disk, and one per keystroke
// would take the daemon's few reads from the query that is still typed.
const SEARCH_DEBOUNCE_MS = 250;

export const useFileBrowserFilteredResource = ({
  basePath,
  caseSensitive = false,
  resource,
  searchQuery,
}: UseFileBrowserFilteredResourceParams) => {
  // Search is a privileged route; without sudo the listing stays unfiltered.
  const { privileged } = useAuth();
  // The field shows every keystroke; the search follows once they settle,
  // and a query cleared below three characters ends it at once. Such a query
  // also settles at once, so that one typed over it searches nothing until
  // it settles itself, rather than the query it replaced. The folder settles
  // too, so the render that moves to another folder, before its query is
  // cleared, asks nothing new.
  const typedSearchable = isSearchableQuery(searchQuery);
  const settledQuery = useDebouncedValue(
    searchQuery,
    typedSearchable ? SEARCH_DEBOUNCE_MS : 0,
  );
  const settledBase = useDebouncedValue(basePath, SEARCH_DEBOUNCE_MS);
  const query = typedSearchable ? settledQuery : searchQuery;
  const searchEnabled =
    privileged && typedSearchable && isSearchableQuery(query);
  const {
    results: searchResults,
    error: queryError,
    isLoading: isSearchLoading,
    walk,
  } = useFileSearch({
    query,
    basePath: settledBase,
    caseSensitive,
    enabled: searchEnabled,
  });
  const searchError = queryError;

  const filteredResource = useMemo(() => {
    if (!resource || !searchEnabled) {
      return resource;
    }
    if (resource.type !== "directory" || !resource.items) {
      return resource;
    }

    if (searchError) {
      return resource;
    }

    if (searchResults.length > 0) {
      const items = searchResults.map((result) => {
        const isDirectory = result.isDir;

        return {
          name: result.name,
          path: result.path,
          size: result.size,
          type: isDirectory ? "directory" : "file",
          modTime: result.mod_time,
          canOpenAsText: result.canOpenAsText,
          isRegularFile: result.isRegularFile,
          isDirectory,
          extension: isDirectory ? "" : result.name.split(".").pop() || "",
          showFullPath: true,
        };
      });

      return {
        ...resource,
        items,
      };
    }

    return {
      ...resource,
      items: [],
    };
  }, [resource, searchEnabled, searchError, searchResults]);

  return {
    filteredResource,
    searchError,
    isSearchLoading: resource?.type === "directory" && isSearchLoading,
    // The folder was too large to read whole on disk in time: the results
    // are the index's with what was read, and may miss recent changes.
    isSearchPartial:
      resource?.type === "directory" &&
      searchEnabled &&
      !searchError &&
      walk === "partial",
  };
};
