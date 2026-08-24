import { useCallback, useMemo, useRef, useState } from "react";

import type { FileExtractRequest } from "@/api";
import {
  ensureTarGzExtension,
  ensureZipExtension,
  isArchiveFile,
  stripArchiveExtension,
} from "@/components/filebrowser/utils";
import { useScopedToast } from "@/hooks/useScopedToast";
import type { FileItem, FileResource } from "@/types/filebrowser";
import { splitName, stripNumericSuffix } from "@/utils/fileUpload";
import { withPromiseCleanup } from "@/utils/withPromiseCleanup";

import { useFilePathUtilities } from "./useFilePathUtilities";

interface CompressPayload {
  archiveName?: string;
  destination?: string;
  paths: string[];
}

type ExtractPayload = FileExtractRequest;

interface CompressFormatDialogState {
  baseName: string;
  paths: string[];
}

interface UseFileBrowserArchiveActionsParams {
  compressItems: (payload: CompressPayload) => Promise<unknown>;
  extractArchive: (payload: ExtractPayload) => Promise<unknown>;
  normalizedPath: string;
  onContextMenuClose: () => void;
  resource?: FileResource;
  selectedItems: FileItem[];
  selectedPaths: Set<string>;
}

export const useFileBrowserArchiveActions = ({
  compressItems,
  extractArchive,
  normalizedPath,
  onContextMenuClose,
  resource,
  selectedItems,
  selectedPaths,
}: UseFileBrowserArchiveActionsParams) => {
  const toast = useScopedToast({
    label: "Open files",
    params: { _splat: "" },
    to: "/filebrowser/$",
  });
  const { joinPath } = useFilePathUtilities();
  const [compressFormatDialog, setCompressFormatDialog] =
    useState<CompressFormatDialogState | null>(null);
  const pendingArchiveNamesRef = useRef<Set<string>>(new Set());

  const existingNames = useMemo(
    () => new Set(resource?.items?.map((item) => item.name) ?? []),
    [resource],
  );
  const archiveSelection = useMemo(
    () =>
      selectedItems.length === 1 && isArchiveFile(selectedItems[0].name)
        ? selectedItems[0]
        : null,
    [selectedItems],
  );

  const getUniqueName = useCallback(
    (baseName: string, additionalNames?: Set<string>) => {
      const nameSet = new Set(existingNames);
      additionalNames?.forEach((name) => nameSet.add(name));

      const { base, ext } = splitName(baseName);
      const { root } = stripNumericSuffix(base);
      let hasPlain = false;
      let maxSuffix = 0;

      nameSet.forEach((name) => {
        const { base: candidateBase, ext: candidateExt } = splitName(name);
        if (candidateExt !== ext) {
          return;
        }
        const { root: candidateRoot, suffix } =
          stripNumericSuffix(candidateBase);
        if (candidateRoot !== root) {
          return;
        }
        if (suffix === null) {
          hasPlain = true;
        } else if (suffix > maxSuffix) {
          maxSuffix = suffix;
        }
      });

      if (!hasPlain && !nameSet.has(baseName)) {
        return baseName;
      }
      return `${root} (${maxSuffix + 1})${ext}`;
    },
    [existingNames],
  );

  const handleCloseCompressFormatDialog = useCallback(() => {
    setCompressFormatDialog(null);
  }, []);

  const handleCompressSelection = useCallback(() => {
    onContextMenuClose();
    const paths = Array.from(selectedPaths);
    if (!paths.length) return;
    const baseName =
      selectedItems.length === 1
        ? stripArchiveExtension(selectedItems[0].name)
        : "archive";
    setCompressFormatDialog({ paths, baseName: baseName || "archive" });
  }, [onContextMenuClose, selectedItems, selectedPaths]);

  const handleCompressConfirm = useCallback(
    async (format: "zip" | "tar.gz") => {
      if (!compressFormatDialog) return;
      const { paths, baseName } = compressFormatDialog;
      const pendingNames = pendingArchiveNamesRef.current;
      const archiveName = getUniqueName(
        format === "tar.gz"
          ? ensureTarGzExtension(baseName)
          : ensureZipExtension(baseName),
        pendingNames,
      );
      pendingNames.add(archiveName);
      return withPromiseCleanup(
        (async () => {
          try {
            await compressItems({
              paths,
              archiveName,
              destination: normalizedPath,
            });
          } catch (error) {
            if (!(error instanceof Error && error.name === "AbortError")) {
              const message =
                error instanceof Error && error.message
                  ? error.message
                  : "Failed to create archive";
              toast.error(message);
            }
          }
        })(),
        () => {
          pendingArchiveNamesRef.current.delete(archiveName);
        },
      );
    },
    [compressFormatDialog, compressItems, getUniqueName, normalizedPath, toast],
  );

  const handleExtractSelection = useCallback(async () => {
    onContextMenuClose();
    if (!archiveSelection) return;
    const targetFolder = getUniqueName(
      stripArchiveExtension(archiveSelection.name) || "extracted",
    );
    const destination = joinPath(normalizedPath, targetFolder);
    try {
      await extractArchive({
        archivePath: archiveSelection.path,
        destination,
      });
    } catch {
      // Errors are surfaced via toast in the mutation.
    }
  }, [
    archiveSelection,
    extractArchive,
    getUniqueName,
    joinPath,
    normalizedPath,
    onContextMenuClose,
  ]);

  return {
    canCompressSelection: selectedPaths.size > 0,
    canExtractSelection: Boolean(archiveSelection),
    compressFormatDialog,
    handleCloseCompressFormatDialog,
    handleCompressConfirm,
    handleCompressSelection,
    handleExtractSelection,
  };
};
