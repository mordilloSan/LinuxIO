import React, { ReactNode } from "react";

import { FileDropOverlay, IndexerUnavailableAlert } from "./FileBrowserPanels";

import BreadcrumbsNav from "@/components/filebrowser/Breadcrumbs";
import DirectoryListing from "@/components/filebrowser/DirectoryListing";
import ErrorState from "@/components/filebrowser/ErrorState";
import FileBrowserHeader from "@/components/filebrowser/FileBrowserHeader";
import FileDetail from "@/components/filebrowser/FileDetail";
import SortBar from "@/components/filebrowser/SortBar";
import PageLoader from "@/components/loaders/PageLoader";
import { useAppTheme } from "@/theme";
import type {
  FileItem,
  FileResource,
  SortField,
  SortOrder,
  ViewMode,
} from "@/types/filebrowser";

export interface FileBrowserSurfaceProps {
  isDragOver: boolean;
  onContextMenu: React.MouseEventHandler<HTMLDivElement>;
  onDragEnter: React.DragEventHandler<HTMLDivElement>;
  onDragLeave: React.DragEventHandler<HTMLDivElement>;
  onDragOver: React.DragEventHandler<HTMLDivElement>;
  onDrop: React.DragEventHandler<HTMLDivElement>;
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
  viewIcon: ReactNode;
  viewMode: ViewMode;
}

export interface FileBrowserDataProps {
  errorMessage?: string | null;
  filteredResource?: FileResource;
  isPending: boolean;
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
  chrome: FileBrowserChromeProps;
  data: FileBrowserDataProps;
  file: FileBrowserFileProps;
  listing: FileBrowserListingProps;
  surface: FileBrowserSurfaceProps;
}

const FileBrowserContent: React.FC<FileBrowserContentProps> = ({
  chrome,
  data,
  file,
  listing,
  surface,
}) => {
  const theme = useAppTheme();

  return (
    <div
      data-allow-context-menu="true"
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
          isSaving={chrome.isSavingFile}
          onSearchChange={chrome.onSearchChange}
          onSwitchView={chrome.onSwitchView}
          onToggleHiddenFiles={chrome.onToggleHiddenFiles}
          searchQuery={chrome.searchQuery}
          showHiddenFiles={chrome.showHiddenFiles}
          viewIcon={chrome.viewIcon}
          viewMode={chrome.viewMode}
        />
      )}

      {!chrome.indexerEnabled && !chrome.editingPath && (
        <IndexerUnavailableAlert status={chrome.indexerStatus} />
      )}

      <div
        style={{
          paddingLeft: chrome.editingPath ? 0 : theme.spacing(2),
          paddingRight: chrome.editingPath ? 0 : theme.spacing(2),
          flex: 1,
          minHeight: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        {!chrome.editingPath && (
          <>
            <BreadcrumbsNav
              onNavigate={chrome.onOpenDirectory}
              path={chrome.normalizedPath}
            />

            {!data.isPending &&
              !data.errorMessage &&
              data.resource &&
              data.resource.type === "directory" && (
                <SortBar
                  onSortChange={chrome.onSortChange}
                  sortOrder={chrome.sortOrder}
                />
              )}
          </>
        )}
        <div
          style={{
            paddingLeft: chrome.editingPath ? 0 : theme.spacing(2),
            paddingRight: chrome.editingPath ? 0 : theme.spacing(2),
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
            position: "relative",
          }}
        >
          {data.isPending && <PageLoader />}

          {!data.isPending && data.errorMessage && (
            <ErrorState
              message={data.errorMessage}
              onReset={() => chrome.onOpenDirectory("/")}
            />
          )}

          {!chrome.editingPath &&
            !data.isPending &&
            !data.errorMessage &&
            data.filteredResource &&
            data.filteredResource.type === "directory" && (
              <DirectoryListing
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
                resource={data.filteredResource}
                selectedPaths={listing.selectedPaths}
                showHiddenFiles={listing.showHiddenFiles}
                sortField={listing.sortField}
                sortOrder={listing.sortOrder}
                viewMode={listing.viewMode}
              />
            )}

          {!chrome.editingPath &&
            !data.isPending &&
            !data.errorMessage &&
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

export default React.memo(FileBrowserContent);
