import ArchiveIcon from "@mui/icons-material/Archive";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";
import ContentCutIcon from "@mui/icons-material/ContentCut";
import ContentPasteIcon from "@mui/icons-material/ContentPaste";
import CreateNewFolderIcon from "@mui/icons-material/CreateNewFolder";
import DeleteIcon from "@mui/icons-material/Delete";
import DownloadIcon from "@mui/icons-material/Download";
import DriveFileRenameOutlineIcon from "@mui/icons-material/DriveFileRenameOutline";
import NoteAddIcon from "@mui/icons-material/NoteAdd";
import SecurityIcon from "@mui/icons-material/Security";
import UnarchiveIcon from "@mui/icons-material/Unarchive";
import UploadIcon from "@mui/icons-material/Upload";
import VisibilityIcon from "@mui/icons-material/Visibility";
import {
  Menu,
  MenuItem,
  ListItemIcon,
  ListItemText,
  Divider,
} from "@mui/material";
import React, { useEffect, useRef } from "react";

interface ContextMenuProps {
  anchorPosition: { top: number; left: number } | null;
  hasSelection: boolean;
  hasClipboard?: boolean;
  canShowDetails?: boolean;
  onClose: () => void;
  onCreateFile: () => void;
  onCreateFolder: () => void;
  onChangePermissions: () => void;
  onCopy: () => void;
  onCut: () => void;
  onPaste: () => void;
  onDelete: () => void;
  onDownload: () => void;
  onUpload: () => void;
  onRename: () => void;
  onShowDetails?: () => void;
  onCompress?: () => void;
  onExtract?: () => void;
  canCompress?: boolean;
  canExtract?: boolean;
  canRename?: boolean;
}

const ContextMenu: React.FC<ContextMenuProps> = ({
  anchorPosition,
  hasSelection,
  hasClipboard = false,
  canShowDetails,
  onClose,
  onCreateFile,
  onCreateFolder,
  onChangePermissions,
  onCopy,
  onCut,
  onPaste,
  onDelete,
  onDownload,
  onUpload,
  onRename,
  onShowDetails = () => {},
  onCompress = () => {},
  onExtract = () => {},
  canCompress,
  canExtract,
  canRename,
}) => {
  const menuRef = useRef<HTMLDivElement>(null);
  const detailsDisabled =
    canShowDetails === undefined ? !hasSelection : !canShowDetails;
  const compressDisabled =
    canCompress === undefined ? !hasSelection : !canCompress;
  const extractDisabled = canExtract === undefined ? true : !canExtract;
  const renameDisabled =
    canRename === undefined ? !hasSelection : !canRename;

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

      <MenuItem onClick={onCut} disabled={!hasSelection}>
        <ListItemIcon>
          <ContentCutIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Cut</ListItemText>
      </MenuItem>

      <MenuItem onClick={onRename} disabled={renameDisabled}>
        <ListItemIcon>
          <DriveFileRenameOutlineIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Rename</ListItemText>
      </MenuItem>

      <MenuItem onClick={onPaste} disabled={!hasClipboard}>
        <ListItemIcon>
          <ContentPasteIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Paste</ListItemText>
      </MenuItem>

      <Divider />

      <MenuItem onClick={onDownload} disabled={!hasSelection}>
        <ListItemIcon>
          <DownloadIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Download</ListItemText>
      </MenuItem>

      <MenuItem onClick={onShowDetails} disabled={detailsDisabled}>
        <ListItemIcon>
          <VisibilityIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Show Details</ListItemText>
      </MenuItem>

      <MenuItem onClick={onCompress} disabled={compressDisabled}>
        <ListItemIcon>
          <ArchiveIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Compress to ZIP</ListItemText>
      </MenuItem>

      <MenuItem onClick={onExtract} disabled={extractDisabled}>
        <ListItemIcon>
          <UnarchiveIcon fontSize="small" />
        </ListItemIcon>
        <ListItemText>Extract Here</ListItemText>
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
