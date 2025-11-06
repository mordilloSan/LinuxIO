import React from "react";

import { FileItem } from "../../types/filebrowser";

import FileCard from "@/components/filebrowser/FileCard";

interface FilesListProps {
  files: FileItem[];
  selectedPaths: Set<string>;
  onFileClick: (event: React.MouseEvent, path: string) => void;
  onDownloadFile: (item: FileItem) => void;
  onFileContextMenu: (event: React.MouseEvent, path: string) => void;
}

const FilesList: React.FC<FilesListProps> = React.memo(
  ({
    files,
    selectedPaths,
    onFileClick,
    onDownloadFile,
    onFileContextMenu,
  }) => {
    if (files.length === 0) {
      return null;
    }

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
            display: "grid",
            gridTemplateColumns:
              "repeat(auto-fill, minmax(min(260px, 100%), 1fr))",
            gap: "12px",
          }}
        >
          {files.map((file) => (
            <FileCard
              key={`${file.path}-${file.name}`}
              name={file.name}
              type={file.type}
              size={file.size}
              modTime={file.modTime}
              isDirectory={false}
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
