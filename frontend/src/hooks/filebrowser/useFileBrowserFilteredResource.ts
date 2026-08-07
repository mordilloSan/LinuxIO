import { useMemo } from "react";

import { useFileSearch } from "@/hooks/filebrowser/useFileSearch";
import type { FileResource } from "@/types/filebrowser";

interface UseFileBrowserFilteredResourceParams {
  resource?: FileResource;
  searchQuery: string;
}

export const useFileBrowserFilteredResource = ({
  resource,
  searchQuery,
}: UseFileBrowserFilteredResourceParams) => {
  const {
    results: searchResults,
    isLoading: isSearchLoading,
    isUnavailable: isSearchUnavailable,
  } = useFileSearch({
    query: searchQuery,
    basePath: "/",
    enabled: searchQuery.trim().length >= 2,
  });

  const filteredResource = useMemo(() => {
    if (!resource || !searchQuery.trim()) {
      return resource;
    }
    if (resource.type !== "directory" || !resource.items) {
      return resource;
    }

    if (isSearchUnavailable) {
      const lowerQuery = searchQuery.toLowerCase().trim();
      const filteredItems = resource.items.filter((item) =>
        item.name.toLowerCase().includes(lowerQuery),
      );
      return {
        ...resource,
        items: filteredItems,
      };
    }

    if (searchResults.length > 0) {
      const items = searchResults.map((result) => {
        const isDirectory = result.isDir;

        return {
          name: result.name,
          path: result.path,
          size: result.size,
          type: result.type,
          modTime: result.mod_time,
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
  }, [resource, searchQuery, searchResults, isSearchUnavailable]);

  return {
    filteredResource,
    isSearchLoading: resource?.type === "directory" && isSearchLoading,
  };
};
