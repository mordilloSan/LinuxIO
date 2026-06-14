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

interface FileBrowserContentProps {
  contextMenuOpen: boolean;
  cutPaths: Set<string>;
  editingPath: string | null;
  errorMessage?: string | null;
  filteredResource?: FileResource;
  indexerEnabled: boolean;
  indexerStatus: string;
  isDragOver: boolean;
  isPending: boolean;
  isSavingFile: boolean;
  normalizedPath: string;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void | Promise<void>;
  onContextMenu: React.MouseEventHandler<HTMLDivElement>;
  onDelete: () => void;
  onDownloadCurrent: (path: string) => void;
  onDownloadFile: (item: FileItem) => void;
  onDragEnter: React.DragEventHandler<HTMLDivElement>;
  onDragLeave: React.DragEventHandler<HTMLDivElement>;
  onDragOver: React.DragEventHandler<HTMLDivElement>;
  onDrop: React.DragEventHandler<HTMLDivElement>;
  onEditFile: (path: string) => void;
  onOpenDirectory: (path: string) => void;
  onSearchChange: (value: string) => void;
  onSelectedPathsChange: (paths: Set<string>) => void;
  onSortChange: (field: SortField) => void;
  onStartRename: () => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  renamingPath: string | null;
  resource?: FileResource;
  searchQuery: string;
  selectedPaths: Set<string>;
  showHiddenFiles: boolean;
  sortField: SortField;
  sortOrder: SortOrder;
  viewIcon: ReactNode;
  viewMode: ViewMode;
}

const FileBrowserContent: React.FC<FileBrowserContentProps> = ({
  contextMenuOpen,
  cutPaths,
  editingPath,
  errorMessage,
  filteredResource,
  indexerEnabled,
  indexerStatus,
  isDragOver,
  isPending,
  isSavingFile,
  normalizedPath,
  onCancelRename,
  onConfirmRename,
  onContextMenu,
  onDelete,
  onDownloadCurrent,
  onDownloadFile,
  onDragEnter,
  onDragLeave,
  onDragOver,
  onDrop,
  onEditFile,
  onOpenDirectory,
  onSearchChange,
  onSelectedPathsChange,
  onSortChange,
  onStartRename,
  onSwitchView,
  onToggleHiddenFiles,
  renamingPath,
  resource,
  searchQuery,
  selectedPaths,
  showHiddenFiles,
  sortField,
  sortOrder,
  viewIcon,
  viewMode,
}) => {
  const theme = useAppTheme();

  return (
    <div
      data-allow-context-menu="true"
      onContextMenu={onContextMenu}
      onDragEnter={onDragEnter}
      onDragLeave={onDragLeave}
      onDragOver={onDragOver}
      onDrop={onDrop}
      style={{
        height: "100%",
        width: "100%",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        position: "relative",
      }}
    >
      {!editingPath && (
        <FileBrowserHeader
          isSaving={isSavingFile}
          onSearchChange={onSearchChange}
          onSwitchView={onSwitchView}
          onToggleHiddenFiles={onToggleHiddenFiles}
          searchQuery={searchQuery}
          showHiddenFiles={showHiddenFiles}
          viewIcon={viewIcon}
          viewMode={viewMode}
        />
      )}

      {!indexerEnabled && !editingPath && (
        <IndexerUnavailableAlert status={indexerStatus} />
      )}

      <div
        style={{
          paddingLeft: editingPath ? 0 : theme.spacing(2),
          paddingRight: editingPath ? 0 : theme.spacing(2),
          flex: 1,
          minHeight: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        {!editingPath && (
          <>
            <BreadcrumbsNav
              onNavigate={onOpenDirectory}
              path={normalizedPath}
            />

            {!isPending &&
              !errorMessage &&
              resource &&
              resource.type === "directory" && (
                <SortBar onSortChange={onSortChange} sortOrder={sortOrder} />
              )}
          </>
        )}
        <div
          style={{
            paddingLeft: editingPath ? 0 : theme.spacing(2),
            paddingRight: editingPath ? 0 : theme.spacing(2),
            flex: 1,
            minHeight: 0,
            display: "flex",
            flexDirection: "column",
            position: "relative",
          }}
        >
          {isPending && <PageLoader />}

          {!isPending && errorMessage && (
            <ErrorState
              message={errorMessage}
              onReset={() => onOpenDirectory("/")}
            />
          )}

          {!editingPath &&
            !isPending &&
            !errorMessage &&
            filteredResource &&
            filteredResource.type === "directory" && (
              <DirectoryListing
                cutPaths={cutPaths}
                isContextMenuOpen={contextMenuOpen}
                key={normalizedPath}
                onCancelRename={onCancelRename}
                onConfirmRename={onConfirmRename}
                onDelete={onDelete}
                onDownloadFile={onDownloadFile}
                onOpenDirectory={onOpenDirectory}
                onSelectedPathsChange={onSelectedPathsChange}
                onStartRename={onStartRename}
                renamingPath={renamingPath}
                resource={filteredResource}
                selectedPaths={selectedPaths}
                showHiddenFiles={showHiddenFiles}
                sortField={sortField}
                sortOrder={sortOrder}
                viewMode={viewMode}
              />
            )}

          {!editingPath &&
            !isPending &&
            !errorMessage &&
            resource &&
            resource.type !== "directory" && (
              <FileDetail
                onDownload={onDownloadCurrent}
                onEdit={onEditFile}
                resource={resource}
              />
            )}
        </div>
      </div>

      {isDragOver && !editingPath && resource?.type === "directory" && (
        <FileDropOverlay normalizedPath={normalizedPath} />
      )}
    </div>
  );
};

export default FileBrowserContent;
