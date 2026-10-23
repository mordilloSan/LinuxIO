export const ensureTrailingSlash = (path: string): string =>
  path.endsWith("/") ? path : `${path}/`;

export const joinPath = (base: string, segment: string): string =>
  `${ensureTrailingSlash(base)}${segment}`;

export const stripTrailingSlash = (path: string): string =>
  path.length > 1 ? path.replace(/\/+$/, "") : path;

export const isDirectoryPath = (path: string): boolean => path.endsWith("/");

/** Last path segment ("/a/b/c.txt" -> "c.txt", "/a/b/" -> "b", "/" -> ""). */
export const baseName = (path: string): string => {
  const parts = stripTrailingSlash(path).split("/").filter(Boolean);
  return parts.length ? parts[parts.length - 1] : "";
};

/** Parent directory ("/a/b/c.txt" -> "/a/b", "/a/b/" -> "/a", "/" -> "/"). */
export const parentPath = (path: string): string => {
  const trimmed = stripTrailingSlash(path);
  const lastSlash = trimmed.lastIndexOf("/");
  if (lastSlash <= 0) return "/";
  return trimmed.slice(0, lastSlash) || "/";
};
