import { useMemo } from "react";

import { useFileSearch } from "@/hooks/filebrowser/useFileSearch";
import type { FileResource } from "@/types/filebrowser";
import { isDirectoryPath } from "@/utils/path";

interface UseFileBrowserFilteredResourceParams {
  resource?: FileResource;
  searchQuery: string;
}

export const useFileBrowserFilteredResource = ({
  resource,
  searchQuery,
}: UseFileBrowserFilteredResourceParams): FileResource | undefined => {
  const { results: searchResults, isUnavailable: isSearchUnavailable } =
    useFileSearch({
      query: searchQuery,
      basePath: "/",
      enabled: searchQuery.trim().length >= 2,
    });

  return useMemo(() => {
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
        const normalizedType =
          typeof result.type === "string" ? result.type.toLowerCase() : "";
        const isDirectory =
          normalizedType === "directory" ||
          normalizedType === "dir" ||
          normalizedType === "folder" ||
          Boolean(result.isDir) ||
          isDirectoryPath(result.path);

        return {
          name: result.name,
          path: result.path,
          size: result.size,
          type: isDirectory
            ? "directory"
            : normalizedType && normalizedType !== "file"
              ? (result.type ?? "file")
              : "file",
          modTime: result.mod_time || result.modTime || result.modified || "",
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
};
