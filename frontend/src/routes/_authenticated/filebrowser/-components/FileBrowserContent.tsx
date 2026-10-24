import {
  memo,
  type DragEventHandler,
  type MouseEventHandler,
  type ReactNode,
} from "react";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import { allowContextMenuProps } from "@/utils/contextMenu";

import DirectoryListing from "./DirectoryListing";
import FileBrowserHeader from "./FileBrowserHeader";
import { FileDropOverlay } from "./FileBrowserPanels";
import FileDetail from "./FileDetail";
import SortBar from "./SortBar";
import type {
  FileItem,
  FileResource,
  SortField,
  SortOrder,
  ViewMode,
} from "../-types";

interface FileBrowserSurfaceProps {
  isDragOver: boolean;
  onContextMenu: MouseEventHandler<HTMLDivElement>;
  onDragEnter: DragEventHandler<HTMLDivElement>;
  onDragLeave: DragEventHandler<HTMLDivElement>;
  onDragOver: DragEventHandler<HTMLDivElement>;
  onDrop: DragEventHandler<HTMLDivElement>;
}

interface FileBrowserChromeProps {
  editingPath: string | null;
  isSavingFile: boolean;
  normalizedPath: string;
  onDropItems: (paths: string[], destinationDir: string, copy: boolean) => void;
  onOpenDirectory: (path: string) => void;
  onSearchCaseSensitiveChange: (value: boolean) => void;
  onSearchChange: (value: string) => void;
  onSortChange: (field: SortField) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  searchQuery: string;
  searchCaseSensitive: boolean;
  showHiddenFiles: boolean;
  sortOrder: SortOrder;
  viewMode: ViewMode;
}

interface FileBrowserDataProps {
  filteredResource?: FileResource;
  searchError?: Error | null;
  isSearchLoading: boolean;
  resource?: FileResource;
}

interface FileBrowserListingProps {
  contextMenuOpen: boolean;
  cutPaths: Set<string>;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void | Promise<void>;
  onDelete: () => void;
  onDeletePermanently: () => void;
  onDownloadFile: (item: FileItem) => void;
  onDropItems: (paths: string[], destinationDir: string, copy: boolean) => void;
  onNavigateUp: () => void;
  onOpenDirectory: (path: string) => void;
  onSelectedPathsChange: (paths: Set<string>) => void;
  onStartRename: () => void;
  renamingPath: string | null;
  renamePendingPath?: string | null;
  renameProgressPct?: number;
  selectedPaths: Set<string>;
  showHiddenFiles: boolean;
  sortField: SortField;
  sortOrder: SortOrder;
  viewMode: ViewMode;
}

interface FileBrowserFileProps {
  onDownloadCurrent: (path: string) => void;
  onEditFile: (path: string) => void;
}

export interface FileBrowserContentProps {
  breadcrumbs: ReactNode;
  chrome: FileBrowserChromeProps;
  data: FileBrowserDataProps;
  file: FileBrowserFileProps;
  listing: FileBrowserListingProps;
  surface: FileBrowserSurfaceProps;
}

const FileBrowserContent = ({
  breadcrumbs,
  chrome,
  data,
  file,
  listing,
  surface,
}: FileBrowserContentProps) => {
  return (
    <div
      {...allowContextMenuProps}
      onContextMenu={surface.onContextMenu}
      onDragEnter={surface.onDragEnter}
      onDragLeave={surface.onDragLeave}
      onDragOver={surface.onDragOver}
      onDrop={surface.onDrop}
      style={{
        height: "100%",
        width: "100%",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        position: "relative",
      }}
    >
      {!chrome.editingPath && (
        <FileBrowserHeader
          breadcrumbs={breadcrumbs}
          isSaving={chrome.isSavingFile}
          onSearchCaseSensitiveChange={chrome.onSearchCaseSensitiveChange}
          onSearchChange={chrome.onSearchChange}
          onSwitchView={chrome.onSwitchView}
          onToggleHiddenFiles={chrome.onToggleHiddenFiles}
          searchQuery={chrome.searchQuery}
          searchCaseSensitive={chrome.searchCaseSensitive}
          showHiddenFiles={chrome.showHiddenFiles}
          viewMode={chrome.viewMode}
        />
      )}

      <div
        style={{
          paddingLeft: chrome.editingPath ? 0 : "var(--app-space-8)",
          paddingRight: chrome.editingPath ? 0 : "var(--app-space-8)",
          flex: 1,
          minHeight: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        {!chrome.editingPath && data.resource?.type === "directory" && (
          <SortBar
            onSortChange={chrome.onSortChange}
            sortField={listing.sortField}
            sortOrder={chrome.sortOrder}
          />
        )}
        <div
          data-app-view-mode-content
          style={{
            paddingLeft: chrome.editingPath ? 0 : "var(--app-space-8)",
            paddingRight: chrome.editingPath ? 0 : "var(--app-space-8)",
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
            position: "relative",
          }}
        >
          {data.isSearchLoading && <ComponentLoader />}

          {!chrome.editingPath && data.searchError && (
            <AppAlert severity="error">
              <AppAlertTitle>Search unavailable</AppAlertTitle>
              {data.searchError.message}
            </AppAlert>
          )}

          {!chrome.editingPath &&
            !data.isSearchLoading &&
            !data.searchError &&
            data.filteredResource &&
            data.filteredResource.type === "directory" && (
              <DirectoryListing
                // A directory is a lifecycle boundary: reset scroll, focus and
                // range-selection anchors, marquee state, and measured rows.
                cutPaths={listing.cutPaths}
                isContextMenuOpen={listing.contextMenuOpen}
                key={chrome.normalizedPath}
                onCancelRename={listing.onCancelRename}
                onConfirmRename={listing.onConfirmRename}
                onDelete={listing.onDelete}
                onDeletePermanently={listing.onDeletePermanently}
                onDownloadFile={listing.onDownloadFile}
                onDropItems={listing.onDropItems}
                onNavigateUp={listing.onNavigateUp}
                onOpenDirectory={listing.onOpenDirectory}
                onSelectedPathsChange={listing.onSelectedPathsChange}
                onStartRename={listing.onStartRename}
                renamingPath={listing.renamingPath}
                renamePendingPath={listing.renamePendingPath}
                renameProgressPct={listing.renameProgressPct}
                resource={data.filteredResource}
                selectedPaths={listing.selectedPaths}
                showHiddenFiles={listing.showHiddenFiles}
                sortField={listing.sortField}
                sortOrder={listing.sortOrder}
                viewMode={listing.viewMode}
              />
            )}

          {!chrome.editingPath &&
            data.resource &&
            data.resource.type !== "directory" && (
              <FileDetail
                onDownload={file.onDownloadCurrent}
                onEdit={file.onEditFile}
                resource={data.resource}
              />
            )}
        </div>
      </div>

      {surface.isDragOver &&
        !chrome.editingPath &&
        data.resource?.type === "directory" && (
          <FileDropOverlay normalizedPath={chrome.normalizedPath} />
        )}
    </div>
  );
};

export default memo(FileBrowserContent);
