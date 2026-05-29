import React from "react";

import { FileItem, ViewMode } from "../../types/filebrowser";

import FileCard from "@/components/cards/FileCard";
import FileListRow from "@/components/filebrowser/FileListRow";

interface FilesListProps {
  cutPaths: Set<string>;
  files: FileItem[];
  isMarqueeSelecting?: boolean;
  onCancelRename: () => void;
  onConfirmRename: (path: string, newName: string) => void;
  onDownloadFile: (item: FileItem) => void;
  onFileClick: (event: React.MouseEvent, path: string) => void;
  onFileContextMenu: (event: React.MouseEvent, path: string) => void;
  renamingPath: string | null;
  selectedPaths: Set<string>;
  viewMode: ViewMode;
}

const FilesList: React.FC<FilesListProps> = React.memo(
  ({
    files,
    selectedPaths,
    cutPaths,
    viewMode,
    onFileClick,
    onDownloadFile,
    onFileContextMenu,
    isMarqueeSelecting = false,
    renamingPath,
    onConfirmRename,
    onCancelRename,
  }) => {
    if (files.length === 0) {
      return null;
    }

    const ItemComponent = viewMode === "list" ? FileListRow : FileCard;

    return (
      <div>
        <h6
          style={{
            fontWeight: 600,
            fontSize: "15px",
            margin: "0 0 4px 0",
            paddingLeft: "4px",
            paddingRight: "4px",
            color: "inherit",
          }}
        >
          Files
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
          {files.map((file) => (
            <ItemComponent
              disableHover={isMarqueeSelecting}
              hidden={file.hidden}
              isCut={cutPaths.has(file.path)}
              isDirectory={false}
              isRenaming={renamingPath === file.path}
              isSymlink={file.symlink}
              key={`${file.path}-${file.name}`}
              modTime={file.modTime}
              name={file.name}
              onCancelRename={onCancelRename}
              onClick={(event) => onFileClick(event, file.path)}
              onConfirmRename={(newName) => onConfirmRename(file.path, newName)}
              onContextMenu={(event) => onFileContextMenu(event, file.path)}
              onDoubleClick={() => onDownloadFile(file)}
              path={file.path}
              selected={selectedPaths.has(file.path)}
              showFullPath={file.showFullPath}
              size={file.size}
              type={file.type}
            />
          ))}
        </div>
      </div>
    );
  },
);

FilesList.displayName = "FilesList";

export default FilesList;
