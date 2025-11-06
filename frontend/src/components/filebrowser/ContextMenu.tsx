import React, { useEffect, useRef } from "react";
import {
  Menu,
  MenuItem,
  ListItemIcon,
  ListItemText,
  Divider,
} from "@mui/material";
import {
  CreateNewFolder as CreateNewFolderIcon,
  NoteAdd as NoteAddIcon,
  Security as SecurityIcon,
  ContentCopy as ContentCopyIcon,
  DriveFileMove as DriveFileMoveIcon,
  Delete as DeleteIcon,
  Download as DownloadIcon,
  Upload as UploadIcon,
} from "@mui/icons-material";

interface ContextMenuProps {
  anchorPosition: { top: number; left: number } | null;
  hasSelection: boolean;
  onClose: () => void;
  onCreateFile: () => void;
  onCreateFolder: () => void;
  onChangePermissions: () => void;
  onCopy: () => void;
  onMove: () => void;
  onDelete: () => void;
  onDownload: () => void;
  onUpload: () => void;
}

const ContextMenu: React.FC<ContextMenuProps> = ({
  anchorPosition,
  hasSelection,
  onClose,
  onCreateFile,
  onCreateFolder,
  onChangePermissions,
  onCopy,
  onMove,
  onDelete,
  onDownload,
  onUpload,
}) => {
  const menuRef = useRef<HTMLDivElement>(null);

  // Close menu on Escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    };

    if (anchorPosition) {
      document.addEventListener("keydown", handleKeyDown);
      return () => document.removeEventListener("keydown", handleKeyDown);
    }
  }, [anchorPosition, onClose]);

  return (
    <Menu
      ref={menuRef}
      open={Boolean(anchorPosition)}
      onClose={onClose}
      anchorReference="anchorPosition"
      anchorPosition={
        anchorPosition
          ? { top: anchorPosition.top, left: anchorPosition.left }
          : undefined
      }
      slotProps={{
        paper: {
          sx: {
            minWidth: 200,
          },
        },
      }}
    >
      {/* Always available actions */}
      <MenuItem onClick={onCreateFile}>
        <ListItemIcon>
          <NoteAddIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Create File</ListItemText>
      </MenuItem>

      <MenuItem onClick={onCreateFolder}>
        <ListItemIcon>
          <CreateNewFolderIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Create Folder</ListItemText>
      </MenuItem>

      <MenuItem onClick={onUpload}>
        <ListItemIcon>
          <UploadIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Upload</ListItemText>
      </MenuItem>

      <Divider />

      {/* Selection-based actions */}
      <MenuItem onClick={onChangePermissions} disabled={!hasSelection}>
        <ListItemIcon>
          <SecurityIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Change Permissions</ListItemText>
      </MenuItem>

      <MenuItem onClick={onCopy} disabled={!hasSelection}>
        <ListItemIcon>
          <ContentCopyIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Copy</ListItemText>
      </MenuItem>

      <MenuItem onClick={onMove} disabled={!hasSelection}>
        <ListItemIcon>
          <DriveFileMoveIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Move</ListItemText>
      </MenuItem>

      <Divider />

      <MenuItem onClick={onDownload} disabled={!hasSelection}>
        <ListItemIcon>
          <DownloadIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Download</ListItemText>
      </MenuItem>

      <MenuItem onClick={onDelete} disabled={!hasSelection}>
        <ListItemIcon>
          <DeleteIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Delete</ListItemText>
      </MenuItem>
    </Menu>
  );
};

export default ContextMenu;
