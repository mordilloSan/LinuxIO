import { useCallback } from "react";

/**
 * Custom hook for common path manipulation utilities
 * Provides memoized callbacks for path operations
 */
export const useFilePathUtilities = () => {
  /**
   * Join a base path with a name, ensuring proper slash handling
   * @example joinPath("/home/user", "file.txt") => "/home/user/file.txt"
   * @example joinPath("/home/user/", "file.txt") => "/home/user/file.txt"
   */
  const joinPath = useCallback((base: string, name: string) => {
    if (base.endsWith("/")) {
      return `${base}${name}`;
    }
    return `${base}/${name}`;
  }, []);

  /**
   * Get the parent directory path
   * @example getParentPath("/home/user/file.txt") => "/home/user"
   * @example getParentPath("/home/user/") => "/home"
   * @example getParentPath("/") => "/"
   */
  const getParentPath = useCallback((fullPath: string) => {
    const trimmed =
      fullPath.endsWith("/") && fullPath.length > 1
        ? fullPath.replace(/\/+$/, "")
        : fullPath;
    const lastSlash = trimmed.lastIndexOf("/");
    if (lastSlash <= 0) return "/";
    return trimmed.slice(0, lastSlash) || "/";
  }, []);

  /**
   * Get the base name (filename/foldername) from a full path
   * @example getBaseName("/home/user/file.txt") => "file.txt"
   * @example getBaseName("/home/user/") => "user"
   * @example getBaseName("/") => ""
   */
  const getBaseName = useCallback((fullPath: string) => {
    const trimmed =
      fullPath.endsWith("/") && fullPath.length > 1
        ? fullPath.replace(/\/+$/, "")
        : fullPath;
    const parts = trimmed.split("/").filter(Boolean);
    return parts.length ? parts[parts.length - 1] : "";
  }, []);

  return {
    joinPath,
    getParentPath,
    getBaseName,
  };
};
