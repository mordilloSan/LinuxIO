import { useEffect, useMemo, useReducer } from "react";

import { useConfigValue } from "@/hooks/useConfig";
import type { SortField, SortOrder, ViewMode } from "@/types/filebrowser";
import {
  readPersistedState,
  writePersistedState,
} from "@/utils/persistedState";

const viewModes: ViewMode[] = ["card", "list"];

// The filebrowser's card/list toggle is per-browser state, so it lives in
// localStorage rather than the config the other pages' `useViewMode` uses —
// and its "list" mode isn't a `TableCardViewMode` anyway.
const VIEW_MODE_STORAGE_KEY = "linuxio.filebrowserViewMode";

const isViewMode = (value: unknown): value is ViewMode =>
  viewModes.includes(value as ViewMode);

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
 * `showHiddenFiles` writes through to the persisted config, `viewMode` to
 * localStorage. `actions` keeps a stable identity as long as the config
 * setter does, so consumers can hold it in callbacks without churn.
 */
export const useFileViewState = (): ViewSlice => {
  const [state, dispatch] = useReducer(
    viewReducer,
    initialViewState,
    (base) => ({
      ...base,
      viewMode:
        readPersistedState(VIEW_MODE_STORAGE_KEY, isViewMode) ?? base.viewMode,
    }),
  );
  const [showHiddenFiles, setShowHiddenFiles] =
    useConfigValue("showHiddenFiles");

  useEffect(() => {
    writePersistedState(VIEW_MODE_STORAGE_KEY, state.viewMode);
  }, [state.viewMode]);

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
