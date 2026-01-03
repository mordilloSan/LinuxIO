import { Dispatch, SetStateAction, useCallback, useState } from "react";

import { useConfigValue } from "@/hooks/useConfig";
import type { ViewMode, SortField, SortOrder } from "@/types/filebrowser";

const viewModes: ViewMode[] = ["card", "list"];

interface ContextMenuPosition {
  top: number;
  left: number;
}

interface useFileViewStateResult {
  // View mode
  viewMode: ViewMode;
  setViewMode: Dispatch<SetStateAction<ViewMode>>;
  handleSwitchView: () => void;

  // Hidden files
  showHiddenFiles: boolean;
  setShowHiddenFiles: (show: boolean) => void;
  handleToggleHiddenFiles: () => void;

  // Sorting
  sortField: SortField;
  setSortField: Dispatch<SetStateAction<SortField>>;
  sortOrder: SortOrder;
  setSortOrder: Dispatch<SetStateAction<SortOrder>>;

  // Context menu
  contextMenuPosition: ContextMenuPosition | null;
  setContextMenuPosition: Dispatch<SetStateAction<ContextMenuPosition | null>>;
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
