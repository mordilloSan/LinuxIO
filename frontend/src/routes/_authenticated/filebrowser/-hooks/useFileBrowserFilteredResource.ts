import { useMemo } from "react";

import useAuth from "@/hooks/useAuth";

import type { FileResource } from "../-types";
import { isSearchableQuery, useFileSearch } from "./useFileSearch";

interface UseFileBrowserFilteredResourceParams {
  caseSensitive?: boolean;
  resource?: FileResource;
  searchQuery: string;
}

export const useFileBrowserFilteredResource = ({
  caseSensitive = false,
  resource,
  searchQuery,
}: UseFileBrowserFilteredResourceParams) => {
  // Search is a privileged route; without sudo the listing stays unfiltered.
  const { privileged } = useAuth();
  const searchEnabled = privileged && isSearchableQuery(searchQuery);
  const {
    results: searchResults,
    error: queryError,
    isLoading: isSearchLoading,
  } = useFileSearch({
    query: searchQuery,
    basePath: "/",
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
  };
};
