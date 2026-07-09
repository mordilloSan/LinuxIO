import { useCallback, useMemo, useReducer } from "react";

import type { FileItem, FileResource } from "@/types/filebrowser";

import { useScopedToast } from "@/hooks/useScopedToast";

type ClipboardOperation = "copy" | "cut";

interface ClipboardData {
  operation: ClipboardOperation;
  paths: string[];
}

interface SelectionState {
  clipboard: ClipboardData | null;
  selectedPaths: Set<string>;
}

type SelectionEvent =
  | { type: "clear" }
  | { type: "clearClipboard" }
  | { paths: string[]; type: "copyToClipboard" }
  | { paths: string[]; type: "cutToClipboard" }
  | { paths: Set<string>; type: "select" };

const initialSelectionState: SelectionState = {
  clipboard: null,
  selectedPaths: new Set(),
};

function selectionReducer(
  state: SelectionState,
  event: SelectionEvent,
): SelectionState {
  switch (event.type) {
    case "clear":
      return { ...state, selectedPaths: new Set() };
    case "clearClipboard":
      return { ...state, clipboard: null };
    case "copyToClipboard":
      return { ...state, clipboard: { operation: "copy", paths: event.paths } };
    case "cutToClipboard":
      return { ...state, clipboard: { operation: "cut", paths: event.paths } };
    case "select":
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

interface SelectionSlice extends SelectionState {
  actions: SelectionActions;
  cutPaths: Set<string>;
}

/**
 * Selection slice: the listing's selection and clipboard behind a stable
 * semantic-action API. `actions` never changes identity, so consumers can
 * hold it in callbacks without churn.
 */
export const useFileSelectionState = (): SelectionSlice => {
  const [state, dispatch] = useReducer(selectionReducer, initialSelectionState);

  const actions = useMemo<SelectionActions>(
    () => ({
      clear: () => dispatch({ type: "clear" }),
      clearClipboard: () => dispatch({ type: "clearClipboard" }),
      copyToClipboard: (paths) => dispatch({ paths, type: "copyToClipboard" }),
      cutToClipboard: (paths) => dispatch({ paths, type: "cutToClipboard" }),
      select: (paths) => dispatch({ paths, type: "select" }),
    }),
    [],
  );

  // Cut items render dimmed in the listing until they are pasted.
  const cutPaths = useMemo(() => {
    if (state.clipboard?.operation === "cut") {
      return new Set(state.clipboard.paths);
    }
    return new Set<string>();
  }, [state.clipboard]);

  return { ...state, actions, cutPaths };
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
