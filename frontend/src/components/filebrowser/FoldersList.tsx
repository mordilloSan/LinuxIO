import React from "react";

import FileCard from "@/components/cards/FileCard";
import FileListRow from "@/components/filebrowser/FileListRow";
import { SubfolderData } from "@/hooks/filebrowser/useFileSubfolders";

import { FileItem, ViewMode } from "../../types/filebrowser";

interface FoldersListProps {
  cutPaths: Set<string>;
  folders: FileItem[];
  isLoadingSubfolders: boolean;
  isMarqueeSelecting?: boolean;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void;
  onFolderClick: (event: React.MouseEvent, path: string) => void;
  onFolderContextMenu: (event: React.MouseEvent, path: string) => void;
  onOpenDirectory: (path: string) => void;
  renamingPath: string | null;
  selectedPaths: Set<string>;
  subfoldersMap: Map<string, SubfolderData>;
  viewMode: ViewMode;
}

interface FolderItemProps {
  disableHover?: boolean;
  folder: FileItem;
  isCut: boolean;
  isLoadingSubfolders: boolean;
  isRenaming: boolean;
  onCancelRename: () => void;
  onConfirmRename: (newName: string) => void;
  onFolderClick: (event: React.MouseEvent, path: string) => void;
  onFolderContextMenu: (event: React.MouseEvent, path: string) => void;
  onOpenDirectory: (path: string) => void;
  selected: boolean;
  subfoldersMap: Map<string, SubfolderData>;
  viewMode: ViewMode;
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
        directorySizeError={null}
        directorySizeLoading={sizeIsLoading}
        directorySizeUnavailable={sizeIsUnavailable}
        disableHover={disableHover}
        hidden={folder.hidden}
        isCut={isCut}
        isDirectory={true}
        isRenaming={isRenaming}
        isSymlink={folder.symlink}
        key={`${folder.path}-${folder.name}`}
        modTime={folder.modTime}
        name={folder.name}
        onCancelRename={onCancelRename}
        onClick={(event) => onFolderClick(event, folder.path)}
        onConfirmRename={onConfirmRename}
        onContextMenu={(event) => onFolderContextMenu(event, folder.path)}
        onDoubleClick={() => onOpenDirectory(folder.path)}
        path={folder.path}
        selected={selected}
        showFullPath={folder.showFullPath}
        size={folder.symlink ? undefined : size === null ? undefined : size}
        type={folder.type}
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
              disableHover={isMarqueeSelecting}
              folder={folder}
              isCut={cutPaths.has(folder.path)}
              isLoadingSubfolders={isLoadingSubfolders}
              isRenaming={renamingPath === folder.path}
              key={`${folder.path}-${folder.name}`}
              onCancelRename={onCancelRename}
              onConfirmRename={(newName) =>
                onConfirmRename(folder.path, newName)
              }
              onFolderClick={onFolderClick}
              onFolderContextMenu={onFolderContextMenu}
              onOpenDirectory={onOpenDirectory}
              selected={selectedPaths.has(folder.path)}
              subfoldersMap={subfoldersMap}
              viewMode={viewMode}
            />
          ))}
        </div>
      </div>
    );
  },
);

FoldersList.displayName = "FoldersList";

export default FoldersList;
