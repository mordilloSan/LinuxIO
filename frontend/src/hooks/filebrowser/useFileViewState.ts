import { useMemo, useReducer } from "react";

import type { SortField, SortOrder, ViewMode } from "@/types/filebrowser";

import { useConfigValue } from "@/hooks/useConfig";

const viewModes: ViewMode[] = ["card", "list"];

interface ContextMenuPosition {
  left: number;
  top: number;
}

interface ViewState {
  contextMenuPosition: ContextMenuPosition | null;
  searchQuery: string;
  sortField: SortField;
  sortOrder: SortOrder;
  viewMode: ViewMode;
}

type ViewEvent =
  | { field: SortField; type: "changeSort" }
  | { type: "clearSearch" }
  | { type: "closeContextMenu" }
  | { position: ContextMenuPosition; type: "openContextMenu" }
  | { type: "setSearch"; value: string }
  | { type: "switchView" };

const initialViewState: ViewState = {
  contextMenuPosition: null,
  searchQuery: "",
  sortField: "name",
  sortOrder: "asc",
  viewMode: "card",
};

function viewReducer(state: ViewState, event: ViewEvent): ViewState {
  switch (event.type) {
    case "changeSort":
      // Re-sorting by the active field flips the order; a new field starts asc.
      return state.sortField === event.field
        ? { ...state, sortOrder: state.sortOrder === "asc" ? "desc" : "asc" }
        : { ...state, sortField: event.field, sortOrder: "asc" };
    case "clearSearch":
      return { ...state, searchQuery: "" };
    case "closeContextMenu":
      return { ...state, contextMenuPosition: null };
    case "openContextMenu":
      return { ...state, contextMenuPosition: event.position };
    case "setSearch":
      return { ...state, searchQuery: event.value };
    case "switchView": {
      const index = viewModes.indexOf(state.viewMode);
      return { ...state, viewMode: viewModes[(index + 1) % viewModes.length] };
    }
  }
}

interface ViewActions {
  changeSort: (field: SortField) => void;
  clearSearch: () => void;
  closeContextMenu: () => void;
  openContextMenu: (position: ContextMenuPosition) => void;
  setSearch: (value: string) => void;
  switchView: () => void;
  toggleHiddenFiles: () => void;
}

interface ViewSlice extends ViewState {
  actions: ViewActions;
  showHiddenFiles: boolean;
}

/**
 * View slice: how the listing is presented (view mode, sorting, search,
 * hidden files, context-menu position) behind a stable semantic-action API.
 * `showHiddenFiles` writes through to the persisted config. `actions` keeps
 * a stable identity as long as the config setter does, so consumers can hold
 * it in callbacks without churn.
 */
export const useFileViewState = (): ViewSlice => {
  const [state, dispatch] = useReducer(viewReducer, initialViewState);
  const [showHiddenFiles, setShowHiddenFiles] =
    useConfigValue("showHiddenFiles");

  const actions = useMemo<ViewActions>(
    () => ({
      changeSort: (field) => dispatch({ field, type: "changeSort" }),
      clearSearch: () => dispatch({ type: "clearSearch" }),
      closeContextMenu: () => dispatch({ type: "closeContextMenu" }),
      openContextMenu: (position) =>
        dispatch({ position, type: "openContextMenu" }),
      setSearch: (value) => dispatch({ type: "setSearch", value }),
      switchView: () => dispatch({ type: "switchView" }),
      toggleHiddenFiles: () => setShowHiddenFiles((prev) => !prev),
    }),
    [setShowHiddenFiles],
  );

  return { ...state, actions, showHiddenFiles };
};

export type { ViewActions, ViewSlice };
