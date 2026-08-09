import { memo, type MouseEvent } from "react";

import type { SubfolderData } from "@/api";
import FileCard from "@/components/cards/FileCard";
import FileListRow from "@/components/filebrowser/FileListRow";
import type { FileItem, ViewMode } from "@/types/filebrowser";

export interface DirectoryItemProps {
  disableHover: boolean;
  isCut: boolean;
  isLoadingSubfolders: boolean;
  isRenaming: boolean;
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
  selected: boolean;
  subfolderData?: SubfolderData;
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
    selected,
    isCut,
    isRenaming,
    viewMode,
    onFileClick,
    onDownloadFile,
    onFileContextMenu,
    onFolderClick,
    onOpenDirectory,
    onFolderContextMenu,
    onConfirmRename,
    onCancelRename,
    disableHover,
    subfolderData,
    isLoadingSubfolders,
  }) => {
    const ItemComponent = viewMode === "list" ? FileListRow : FileCard;

    if (itemKind === "file") {
      return (
        <ItemComponent
          disableHover={disableHover}
          hidden={item.hidden}
          isCut={isCut}
          isDirectory={false}
          isRenaming={isRenaming}
          isSymlink={item.symlink}
          modTime={item.modTime}
          name={item.name}
          onCancelRename={onCancelRename}
          onClick={(event) => onFileClick(event, item.path)}
          onConfirmRename={(newName) => onConfirmRename(item.path, newName)}
          onContextMenu={(event) => onFileContextMenu(event, item.path)}
          onDoubleClick={() => onDownloadFile(item)}
          path={item.path}
          selected={selected}
          showFullPath={item.showFullPath}
          size={item.size}
          type={item.type}
        />
      );
    }

    const isSearchResult = item.showFullPath === true;
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
        isCut={isCut}
        isDirectory={true}
        isRenaming={isRenaming}
        isSymlink={item.symlink}
        modTime={item.modTime}
        name={item.name}
        onCancelRename={onCancelRename}
        onClick={(event) => onFolderClick(event, item.path)}
        onConfirmRename={(newName) => onConfirmRename(item.path, newName)}
        onContextMenu={(event) => onFolderContextMenu(event, item.path)}
        onDoubleClick={() => onOpenDirectory(item.path)}
        path={item.path}
        selected={selected}
        showFullPath={item.showFullPath}
        size={item.symlink ? undefined : size === null ? undefined : size}
        type={item.type}
      />
    );
  },
);

DirectoryItem.displayName = "VirtualDirectoryItem";
