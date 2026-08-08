import { memo, type MouseEvent } from "react";

import FileCard from "@/components/cards/FileCard";
import FileListRow from "@/components/filebrowser/FileListRow";
import { SubfolderData } from "@/hooks/filebrowser/useFileSubfolders";
import { FileItem, ViewMode } from "@/types/filebrowser";
import { stripTrailingSlash } from "@/utils/path";

export interface DirectoryItemProps {
  cutPaths: Set<string>;
  disableHover: boolean;
  isLoadingSubfolders: boolean;
  item: FileItem;
  itemKind: "file" | "folder";
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void;
  onDownloadFile: (item: FileItem) => void;
  onFileClick: (event: MouseEvent, path: string) => void;
  onFileContextMenu: (event: MouseEvent, path: string) => void;
  onFolderClick: (event: MouseEvent, path: string) => void;
  onFolderContextMenu: (event: MouseEvent, path: string) => void;
  onOpenDirectory: (path: string) => void;
  renamingPath: string | null;
  selectedPaths: Set<string>;
  subfoldersMap: Map<string, SubfolderData>;
  viewMode: ViewMode;
}

export const SectionHeader = memo<{ label: string; viewMode: ViewMode }>(
  ({ label, viewMode }) => (
    <h6
      style={{
        color: "inherit",
        fontSize: "15px",
        fontWeight: 600,
        margin: viewMode === "list" ? "4px 0" : "4px 0",
        paddingLeft: "4px",
        paddingRight: "4px",
      }}
    >
      {label}
    </h6>
  ),
);

SectionHeader.displayName = "VirtualDirectorySectionHeader";

export const DirectoryItem = memo<DirectoryItemProps>(
  ({
    item,
    itemKind,
    selectedPaths,
    cutPaths,
    viewMode,
    onFileClick,
    onDownloadFile,
    onFileContextMenu,
    onFolderClick,
    onOpenDirectory,
    onFolderContextMenu,
    renamingPath,
    onConfirmRename,
    onCancelRename,
    disableHover,
    subfoldersMap,
    isLoadingSubfolders,
  }) => {
    const ItemComponent = viewMode === "list" ? FileListRow : FileCard;

    if (itemKind === "file") {
      return (
        <ItemComponent
          disableHover={disableHover}
          hidden={item.hidden}
          isCut={cutPaths.has(item.path)}
          isDirectory={false}
          isRenaming={renamingPath === item.path}
          isSymlink={item.symlink}
          modTime={item.modTime}
          name={item.name}
          onCancelRename={onCancelRename}
          onClick={(event) => onFileClick(event, item.path)}
          onConfirmRename={(newName) => onConfirmRename(item.path, newName)}
          onContextMenu={(event) => onFileContextMenu(event, item.path)}
          onDoubleClick={() => onDownloadFile(item)}
          path={item.path}
          selected={selectedPaths.has(item.path)}
          showFullPath={item.showFullPath}
          size={item.size}
          type={item.type}
        />
      );
    }

    const isSearchResult = item.showFullPath === true;
    const normalizedPath = stripTrailingSlash(item.path);
    const subfolderData = item.symlink
      ? null
      : subfoldersMap.get(normalizedPath);
    const size = isSearchResult
      ? typeof item.size === "number"
        ? item.size
        : null
      : subfolderData
        ? subfolderData.size
        : null;
    const shouldShowSize = !item.symlink;
    const sizeIsLoading = shouldShowSize && isLoadingSubfolders;
    const sizeIsUnavailable =
      shouldShowSize && !isLoadingSubfolders && size === null;

    return (
      <ItemComponent
        directorySizeError={null}
        directorySizeLoading={sizeIsLoading}
        directorySizeUnavailable={sizeIsUnavailable}
        disableHover={disableHover}
        hidden={item.hidden}
        isCut={cutPaths.has(item.path)}
        isDirectory={true}
        isRenaming={renamingPath === item.path}
        isSymlink={item.symlink}
        modTime={item.modTime}
        name={item.name}
        onCancelRename={onCancelRename}
        onClick={(event) => onFolderClick(event, item.path)}
        onConfirmRename={(newName) => onConfirmRename(item.path, newName)}
        onContextMenu={(event) => onFolderContextMenu(event, item.path)}
        onDoubleClick={() => onOpenDirectory(item.path)}
        path={item.path}
        selected={selectedPaths.has(item.path)}
        showFullPath={item.showFullPath}
        size={item.symlink ? undefined : size === null ? undefined : size}
        type={item.type}
      />
    );
  },
);

DirectoryItem.displayName = "VirtualDirectoryItem";
