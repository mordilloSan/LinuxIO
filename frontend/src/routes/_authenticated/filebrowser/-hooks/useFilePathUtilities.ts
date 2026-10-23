import { baseName, joinPath, parentPath } from "@/utils/path";

/** Path helpers for file browser hooks; implementations live in `@/utils/path`. */
export const useFilePathUtilities = () => ({
  joinPath,
  getParentPath: parentPath,
  getBaseName: baseName,
});
