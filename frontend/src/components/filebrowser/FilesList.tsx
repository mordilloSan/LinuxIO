import React from "react";

import { FileItem, ViewMode } from "../../types/filebrowser";

import FileCard from "@/components/filebrowser/FileCard";
import FileListRow from "@/components/filebrowser/FileListRow";

interface FilesListProps {
  files: FileItem[];
  selectedPaths: Set<string>;
  viewMode: ViewMode;
  onFileClick: (event: React.MouseEvent, path: string) => void;
  onDownloadFile: (item: FileItem) => void;
  onFileContextMenu: (event: React.MouseEvent, path: string) => void;
}

const FilesList: React.FC<FilesListProps> = React.memo(
  ({
    files,
    selectedPaths,
    viewMode,
    onFileClick,
    onDownloadFile,
    onFileContextMenu,
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
          }}
        >
          {files.map((file) => (
            <ItemComponent
              key={`${file.path}-${file.name}`}
              name={file.name}
              type={file.type}
              size={file.size}
              modTime={file.modTime}
              isDirectory={false}
              isSymlink={file.symlink}
              hidden={file.hidden}
              selected={selectedPaths.has(file.path)}
              onClick={(event) => onFileClick(event, file.path)}
              onDoubleClick={() => onDownloadFile(file)}
              onContextMenu={(event) => onFileContextMenu(event, file.path)}
            />
          ))}
        </div>
      </div>
    );
  },
);

FilesList.displayName = "FilesList";

export default FilesList;
