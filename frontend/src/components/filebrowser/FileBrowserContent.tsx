import {
  memo,
  type DragEventHandler,
  type MouseEventHandler,
  type ReactNode,
} from "react";

import DirectoryListing from "@/components/filebrowser/DirectoryListing";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import SortBar from "@/components/filebrowser/SortBar";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import type {
  FileItem,
  FileResource,
  SortField,
  SortOrder,
  ViewMode,
} from "@/types/filebrowser";
import { allowContextMenuProps } from "@/utils/contextMenu";

import { FileDropOverlay, IndexerUnavailableAlert } from "./FileBrowserPanels";

export interface FileBrowserSurfaceProps {
  isDragOver: boolean;
  onContextMenu: MouseEventHandler<HTMLDivElement>;
  onDragEnter: DragEventHandler<HTMLDivElement>;
  onDragLeave: DragEventHandler<HTMLDivElement>;
  onDragOver: DragEventHandler<HTMLDivElement>;
  onDrop: DragEventHandler<HTMLDivElement>;
}

export interface FileBrowserChromeProps {
  editingPath: string | null;
  indexerEnabled: boolean;
  indexerStatus: string;
  isSavingFile: boolean;
  normalizedPath: string;
  onOpenDirectory: (path: string) => void;
  onSearchChange: (value: string) => void;
  onSortChange: (field: SortField) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  searchQuery: string;
  showHiddenFiles: boolean;
  sortOrder: SortOrder;
  viewMode: ViewMode;
}

export interface FileBrowserDataProps {
  filteredResource?: FileResource;
  isSearchLoading: boolean;
  resource?: FileResource;
}

export interface FileBrowserListingProps {
  contextMenuOpen: boolean;
  cutPaths: Set<string>;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void | Promise<void>;
  onDelete: () => void;
  onDownloadFile: (item: FileItem) => void;
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

export interface FileBrowserFileProps {
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
          onSearchChange={chrome.onSearchChange}
          onSwitchView={chrome.onSwitchView}
          onToggleHiddenFiles={chrome.onToggleHiddenFiles}
          searchQuery={chrome.searchQuery}
          showHiddenFiles={chrome.showHiddenFiles}
          viewMode={chrome.viewMode}
        />
      )}

      {!chrome.indexerEnabled && !chrome.editingPath && (
        <IndexerUnavailableAlert status={chrome.indexerStatus} />
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

          {!chrome.editingPath &&
            !data.isSearchLoading &&
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
                onDownloadFile={listing.onDownloadFile}
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
