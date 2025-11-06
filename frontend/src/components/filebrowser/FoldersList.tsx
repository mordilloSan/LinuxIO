import React from "react";
import FileCard from "@/components/filebrowser/FileCard";
import { FileItem } from "./types";

interface FoldersListProps {
  folders: FileItem[];
  selectedPaths: Set<string>;
  onFolderClick: (event: React.MouseEvent, path: string) => void;
  onOpenDirectory: (path: string) => void;
}

const FoldersList: React.FC<FoldersListProps> = React.memo(({
  folders,
  selectedPaths,
  onFolderClick,
  onOpenDirectory,
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
          display: "grid",
          gridTemplateColumns: "repeat(auto-fill, minmax(min(260px, 100%), 1fr))",
          gap: "12px",
        }}
      >
        {folders.map((folder) => (
          <FileCard
            key={`${folder.path}-${folder.name}`}
            name={folder.name}
            type={folder.type}
            size={folder.size}
            modTime={folder.modTime}
            isDirectory={true}
            hidden={folder.hidden}
            selected={selectedPaths.has(folder.path)}
            onClick={(event) => onFolderClick(event, folder.path)}
            onDoubleClick={() => onOpenDirectory(folder.path)}
          />
        ))}
      </div>
    </div>
  );
});

FoldersList.displayName = "FoldersList";

export default FoldersList;
