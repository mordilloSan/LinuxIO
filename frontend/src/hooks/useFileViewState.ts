import { Dispatch, SetStateAction, useCallback, useState } from "react";

import type { SortField, SortOrder, ViewMode } from "@/types/filebrowser";

import { useConfigValue } from "@/hooks/useConfig";

const viewModes: ViewMode[] = ["card", "list"];

interface ContextMenuPosition {
  left: number;
  top: number;
}

interface useFileViewStateResult {
  // Context menu
  contextMenuPosition: ContextMenuPosition | null;
  handleSwitchView: () => void;
  handleToggleHiddenFiles: () => void;

  setContextMenuPosition: Dispatch<SetStateAction<ContextMenuPosition | null>>;
  setShowHiddenFiles: (show: boolean) => void;
  setSortField: Dispatch<SetStateAction<SortField>>;

  setSortOrder: Dispatch<SetStateAction<SortOrder>>;
  setViewMode: Dispatch<SetStateAction<ViewMode>>;
  // Hidden files
  showHiddenFiles: boolean;
  // Sorting
  sortField: SortField;

  sortOrder: SortOrder;
  // View mode
  viewMode: ViewMode;
}

/**
 * Custom hook for managing file browser view state
 * Handles view mode, sorting, hidden files visibility, and context menu
 */
export const useFileViewState = (): useFileViewStateResult => {
  const [viewMode, setViewMode] = useState<ViewMode>("card");
  const [showHiddenFiles, setShowHiddenFilesConfig] =
    useConfigValue("showHiddenFiles");
  const [sortField, setSortField] = useState<SortField>("name");
  const [sortOrder, setSortOrder] = useState<SortOrder>("asc");
  const [contextMenuPosition, setContextMenuPosition] =
    useState<ContextMenuPosition | null>(null);

  const handleSwitchView = useCallback(() => {
    setViewMode((current) => {
      const index = viewModes.indexOf(current);
      const next = (index + 1) % viewModes.length;
      return viewModes[next];
    });
  }, []);

  const handleToggleHiddenFiles = useCallback(() => {
    setShowHiddenFilesConfig((prev) => !prev);
  }, [setShowHiddenFilesConfig]);

  return {
    viewMode,
    setViewMode,
    handleSwitchView,

    showHiddenFiles,
    setShowHiddenFiles: setShowHiddenFilesConfig,
    handleToggleHiddenFiles,

    sortField,
    setSortField,
    sortOrder,
    setSortOrder,

    contextMenuPosition,
    setContextMenuPosition,
  };
};
