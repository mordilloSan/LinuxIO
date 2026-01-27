import React from "react";

import { FileItem, ViewMode } from "../../types/filebrowser";

import FileCard from "@/components/filebrowser/FileCard";
import FileListRow from "@/components/filebrowser/FileListRow";
import { SubfolderData } from "@/hooks/useFileSubfolders";

interface FoldersListProps {
  folders: FileItem[];
  selectedPaths: Set<string>;
  cutPaths: Set<string>;
  viewMode: ViewMode;
  onFolderClick: (event: React.MouseEvent, path: string) => void;
  onOpenDirectory: (path: string) => void;
  onFolderContextMenu: (event: React.MouseEvent, path: string) => void;
  isMarqueeSelecting?: boolean;
  subfoldersMap: Map<string, SubfolderData>;
  isLoadingSubfolders: boolean;
  renamingPath: string | null;
  onConfirmRename: (path: string, newName: string) => void;
  onCancelRename: () => void;
}

interface FolderItemProps {
  folder: FileItem;
  selected: boolean;
  isCut: boolean;
  isRenaming: boolean;
  viewMode: ViewMode;
  onFolderClick: (event: React.MouseEvent, path: string) => void;
  onOpenDirectory: (path: string) => void;
  onFolderContextMenu: (event: React.MouseEvent, path: string) => void;
  onConfirmRename: (newName: string) => void;
  onCancelRename: () => void;
  disableHover?: boolean;
  subfoldersMap: Map<string, SubfolderData>;
  isLoadingSubfolders: boolean;
}

const FolderItem: React.FC<FolderItemProps> = React.memo(
  ({
    folder,
    selected,
    isCut,
    isRenaming,
    viewMode,
    onFolderClick,
    onOpenDirectory,
    onFolderContextMenu,
    onConfirmRename,
    onCancelRename,
    disableHover = false,
    subfoldersMap,
    isLoadingSubfolders,
  }) => {
    const ItemComponent = viewMode === "list" ? FileListRow : FileCard;

    // Get size from subfoldersMap instead of making individual API calls
    // Normalize path by removing trailing slash for lookup (API returns paths without trailing slashes)
    const isSearchResult = folder.showFullPath === true;
    const normalizedPath = folder.path.endsWith("/")
      ? folder.path.slice(0, -1)
      : folder.path;
    const subfolderData = folder.symlink
      ? null
      : subfoldersMap.get(normalizedPath);
    const size = isSearchResult
      ? typeof folder.size === "number"
        ? folder.size
        : null
      : subfolderData
        ? subfolderData.size
        : null;

    // For symlinks, don't show loading or unavailable states
    // For regular folders, show loading if data hasn't arrived yet, unavailable if it's null after loading
    const shouldShowSize = !folder.symlink;
    const sizeIsLoading = shouldShowSize && isLoadingSubfolders;
    const sizeIsUnavailable =
      shouldShowSize && !isLoadingSubfolders && size === null;

    return (
      <ItemComponent
        key={`${folder.path}-${folder.name}`}
        path={folder.path}
        name={folder.name}
        type={folder.type}
        size={folder.symlink ? undefined : size === null ? undefined : size}
        modTime={folder.modTime}
        isDirectory={true}
        isSymlink={folder.symlink}
        hidden={folder.hidden}
        selected={selected}
        isCut={isCut}
        isRenaming={isRenaming}
        showFullPath={folder.showFullPath}
        directorySizeLoading={sizeIsLoading}
        directorySizeError={null}
        directorySizeUnavailable={sizeIsUnavailable}
        onClick={(event) => onFolderClick(event, folder.path)}
        onDoubleClick={() => onOpenDirectory(folder.path)}
        onContextMenu={(event) => onFolderContextMenu(event, folder.path)}
        onConfirmRename={onConfirmRename}
        onCancelRename={onCancelRename}
        disableHover={disableHover}
      />
    );
  },
);

FolderItem.displayName = "FolderItem";

const FoldersList: React.FC<FoldersListProps> = React.memo(
  ({
    folders,
    selectedPaths,
    cutPaths,
    viewMode,
    onFolderClick,
    onOpenDirectory,
    onFolderContextMenu,
    isMarqueeSelecting = false,
    subfoldersMap,
    isLoadingSubfolders,
    renamingPath,
    onConfirmRename,
    onCancelRename,
  }) => {
    if (folders.length === 0) {
      return null;
    }

    return (
      <div>
        <h6
          style={{
            fontWeight: 600,
            fontSize: "15px",
            margin: "4px 0",
            paddingLeft: "4px",
            paddingRight: "4px",
            color: "inherit",
          }}
        >
          Folders
        </h6>
        <div
          style={{
            display: viewMode === "list" ? "flex" : "grid",
            flexDirection: viewMode === "list" ? "column" : undefined,
            gridTemplateColumns:
              viewMode === "card"
                ? "repeat(auto-fill, minmax(min(260px, 100%), 1fr))"
                : undefined,
            gap: viewMode === "list" ? "2px" : "12px",
            padding: viewMode === "card" ? "4px" : undefined,
          }}
        >
          {folders.map((folder) => (
            <FolderItem
              key={`${folder.path}-${folder.name}`}
              folder={folder}
              selected={selectedPaths.has(folder.path)}
              isCut={cutPaths.has(folder.path)}
              isRenaming={renamingPath === folder.path}
              viewMode={viewMode}
              onFolderClick={onFolderClick}
              onOpenDirectory={onOpenDirectory}
              onFolderContextMenu={onFolderContextMenu}
              onConfirmRename={(newName) => onConfirmRename(folder.path, newName)}
              onCancelRename={onCancelRename}
              disableHover={isMarqueeSelecting}
              subfoldersMap={subfoldersMap}
              isLoadingSubfolders={isLoadingSubfolders}
            />
          ))}
        </div>
      </div>
    );
  },
);

FoldersList.displayName = "FoldersList";

export default FoldersList;
