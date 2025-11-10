import React from "react";

import { FileItem, ViewMode } from "../../types/filebrowser";

import FileCard from "@/components/filebrowser/FileCard";
import FileListRow from "@/components/filebrowser/FileListRow";

interface FoldersListProps {
  folders: FileItem[];
  selectedPaths: Set<string>;
  viewMode: ViewMode;
  onFolderClick: (event: React.MouseEvent, path: string) => void;
  onOpenDirectory: (path: string) => void;
  onFolderContextMenu: (event: React.MouseEvent, path: string) => void;
}

const FoldersList: React.FC<FoldersListProps> = React.memo(
  ({
    folders,
    selectedPaths,
    viewMode,
    onFolderClick,
    onOpenDirectory,
    onFolderContextMenu,
  }) => {
    if (folders.length === 0) {
      return null;
    }

    const ItemComponent = viewMode === "list" ? FileListRow : FileCard;

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
          }}
        >
          {folders.map((folder) => (
            <ItemComponent
              key={`${folder.path}-${folder.name}`}
              path={folder.path}
              name={folder.name}
              type={folder.type}
              size={folder.size}
              modTime={folder.modTime}
              isDirectory={true}
              isSymlink={folder.symlink}
              hidden={folder.hidden}
              selected={selectedPaths.has(folder.path)}
              onClick={(event) => onFolderClick(event, folder.path)}
              onDoubleClick={() => onOpenDirectory(folder.path)}
              onContextMenu={(event) => onFolderContextMenu(event, folder.path)}
            />
          ))}
        </div>
      </div>
    );
  },
);

FoldersList.displayName = "FoldersList";

export default FoldersList;
