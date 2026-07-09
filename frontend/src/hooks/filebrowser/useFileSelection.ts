import { useCallback, useMemo, useReducer } from "react";

import { useScopedToast } from "@/hooks/useScopedToast";
import type { FileItem, FileResource } from "@/types/filebrowser";

type ClipboardOperation = "copy" | "cut";

interface ClipboardData {
  operation: ClipboardOperation;
  paths: string[];
}

interface SelectionState {
  clipboard: ClipboardData | null;
  directoryPath: string;
  selectedPaths: Set<string>;
}

type SelectionEvent =
  | { directoryPath: string; type: "changeDirectory" }
  | { directoryPath: string; type: "clear" }
  | { type: "clearClipboard" }
  | { paths: string[]; type: "copyToClipboard" }
  | { paths: string[]; type: "cutToClipboard" }
  | { directoryPath: string; paths: Set<string>; type: "select" };

function selectionReducer(
  state: SelectionState,
  event: SelectionEvent,
): SelectionState {
  switch (event.type) {
    case "changeDirectory":
      if (state.directoryPath === event.directoryPath) return state;
      return {
        ...state,
        directoryPath: event.directoryPath,
        selectedPaths: new Set(),
      };
    case "clear":
      if (state.directoryPath !== event.directoryPath) return state;
      return { ...state, selectedPaths: new Set() };
    case "clearClipboard":
      return { ...state, clipboard: null };
    case "copyToClipboard":
      return { ...state, clipboard: { operation: "copy", paths: event.paths } };
    case "cutToClipboard":
      return { ...state, clipboard: { operation: "cut", paths: event.paths } };
    case "select":
      if (state.directoryPath !== event.directoryPath) return state;
      return { ...state, selectedPaths: event.paths };
  }
}

interface SelectionActions {
  clear: () => void;
  clearClipboard: () => void;
  copyToClipboard: (paths: string[]) => void;
  cutToClipboard: (paths: string[]) => void;
  select: (paths: Set<string>) => void;
}

interface SelectionSlice {
  actions: SelectionActions;
  clipboard: ClipboardData | null;
  cutPaths: Set<string>;
  selectedPaths: Set<string>;
}

/**
 * Selection slice: selection is scoped to the current directory while the
 * clipboard deliberately survives navigation for cross-directory paste.
 * Actions stay stable until the directory changes.
 */
export const useFileSelectionState = (
  directoryPath: string,
): SelectionSlice => {
  const [storedState, dispatch] = useReducer(selectionReducer, {
    clipboard: null,
    directoryPath,
    selectedPaths: new Set<string>(),
  });
  const directoryChanged = storedState.directoryPath !== directoryPath;
  const state = directoryChanged
    ? {
        ...storedState,
        directoryPath,
        selectedPaths: new Set<string>(),
      }
    : storedState;

  // Normalize during render so a route or browser-history navigation never
  // exposes the previous directory's selection for a committed frame.
  if (directoryChanged) {
    dispatch({ directoryPath, type: "changeDirectory" });
  }

  const actions = useMemo<SelectionActions>(
    () => ({
      clear: () => dispatch({ directoryPath, type: "clear" }),
      clearClipboard: () => dispatch({ type: "clearClipboard" }),
      copyToClipboard: (paths) => dispatch({ paths, type: "copyToClipboard" }),
      cutToClipboard: (paths) => dispatch({ paths, type: "cutToClipboard" }),
      select: (paths) => dispatch({ directoryPath, paths, type: "select" }),
    }),
    [directoryPath],
  );

  // Cut items render dimmed in the listing until they are pasted.
  const cutPaths = useMemo(() => {
    if (state.clipboard?.operation === "cut") {
      return new Set(state.clipboard.paths);
    }
    return new Set<string>();
  }, [state.clipboard]);

  return {
    actions,
    clipboard: state.clipboard,
    cutPaths,
    selectedPaths: state.selectedPaths,
  };
};

interface useFileSelectionParams {
  copyItems: (params: {
    sourcePaths: string[];
    destinationDir: string;
  }) => Promise<void>;
  moveItems: (params: {
    sourcePaths: string[];
    destinationDir: string;
  }) => Promise<void>;
  normalizedPath: string;
  onContextMenuClose?: () => void;
  resource: FileResource | undefined;
  selection: SelectionSlice;
}

/**
 * Custom hook for the file browser's clipboard behaviors: copy/cut the
 * current selection and paste it into the current directory.
 */
export const useFileSelection = ({
  resource,
  normalizedPath,
  copyItems,
  moveItems,
  onContextMenuClose,
  selection,
}: useFileSelectionParams) => {
  const toast = useScopedToast({ href: "/filebrowser", label: "Open files" });
  const { actions, clipboard, selectedPaths } = selection;

  // Memoize selected items from resource
  const selectedItems = useMemo(() => {
    if (!resource || resource.type !== "directory" || !resource.items) {
      return [];
    }
    const itemMap = new Map(resource.items.map((item) => [item.path, item]));
    return Array.from(selectedPaths)
      .map((path) => itemMap.get(path))
      .filter(Boolean) as FileItem[];
  }, [resource, selectedPaths]);

  const handleCopy = useCallback(() => {
    onContextMenuClose?.();
    if (selectedPaths.size === 0) return;
    actions.copyToClipboard(Array.from(selectedPaths));
    toast.success(`${selectedPaths.size} item(s) copied to clipboard`);
  }, [actions, onContextMenuClose, selectedPaths]);

  const handleCut = useCallback(() => {
    onContextMenuClose?.();
    if (selectedPaths.size === 0) return;
    actions.cutToClipboard(Array.from(selectedPaths));
    toast.success(`${selectedPaths.size} item(s) cut to clipboard`);
  }, [actions, onContextMenuClose, selectedPaths]);

  const handlePaste = useCallback(async () => {
    onContextMenuClose?.();
    if (!clipboard) {
      toast.error("Nothing to paste");
      return;
    }

    try {
      if (clipboard.operation === "copy") {
        await copyItems({
          sourcePaths: clipboard.paths,
          destinationDir: normalizedPath,
        });
      } else {
        await moveItems({
          sourcePaths: clipboard.paths,
          destinationDir: normalizedPath,
        });
        // Clear clipboard after cut operation
        actions.clearClipboard();
        actions.clear();
      }
    } catch {
      // Error is handled by the mutation
    }
  }, [
    actions,
    onContextMenuClose,
    clipboard,
    copyItems,
    moveItems,
    normalizedPath,
  ]);

  return {
    handleCopy,
    handleCut,
    handlePaste,
    selectedItems,
  };
};

export type { ClipboardData, SelectionActions, SelectionSlice };
