export const ensureTrailingSlash = (path: string): string =>
  path.endsWith("/") ? path : `${path}/`;

export const joinPath = (base: string, segment: string): string =>
  `${ensureTrailingSlash(base)}${segment}`;

export const stripTrailingSlash = (path: string): string =>
  path.length > 1 ? path.replace(/\/+$/, "") : path;

export const isDirectoryPath = (path: string): boolean => path.endsWith("/");
