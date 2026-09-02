import { useMemo } from "react";

import {
  isSearchableQuery,
  useFileSearch,
} from "@/hooks/filebrowser/useFileSearch";
import type { FileResource } from "@/types/filebrowser";

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
  const searchEnabled = isSearchableQuery(searchQuery);
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
