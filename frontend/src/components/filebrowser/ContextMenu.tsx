import { Icon } from "@iconify/react";
import {
  Menu,
  MenuItem,
  ListItemIcon,
  ListItemText,
  Divider,
} from "@mui/material";
import React, { useEffect, useEffectEvent, useRef } from "react";

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
  onOpenContainingFolder?: () => void;
  canOpenContainingFolder?: boolean;
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
  onOpenContainingFolder = () => {},
  canOpenContainingFolder = false,
}) => {
  const menuRef = useRef<HTMLDivElement>(null);
  const detailsDisabled =
    canShowDetails === undefined ? !hasSelection : !canShowDetails;
  const compressDisabled =
    canCompress === undefined ? !hasSelection : !canCompress;
  const extractDisabled = canExtract === undefined ? true : !canExtract;
  const renameDisabled = canRename === undefined ? !hasSelection : !canRename;
  const isOpen = Boolean(anchorPosition);

  // Close menu on Escape key
  const handleKeyDown = useEffectEvent((e: KeyboardEvent) => {
    if (e.key === "Escape") {
      onClose();
    }
  });

  useEffect(() => {
    if (isOpen) {
      document.addEventListener("keydown", handleKeyDown);
      return () => document.removeEventListener("keydown", handleKeyDown);
    }
  }, [isOpen]);

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
          <Icon icon="mdi:file-plus" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Create File</ListItemText>
      </MenuItem>

      <MenuItem onClick={onCreateFolder}>
        <ListItemIcon>
          <Icon icon="mdi:folder-plus" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Create Folder</ListItemText>
      </MenuItem>

      <MenuItem onClick={onUpload}>
        <ListItemIcon>
          <Icon icon="mdi:upload" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Upload</ListItemText>
      </MenuItem>

      <Divider />

      {/* Open containing folder (search results only) */}
      {canOpenContainingFolder && (
        <MenuItem onClick={onOpenContainingFolder}>
          <ListItemIcon>
            <Icon icon="mdi:folder-open" width={20} height={20} />
          </ListItemIcon>
          <ListItemText>Open Containing Folder</ListItemText>
        </MenuItem>
      )}

      {canOpenContainingFolder && <Divider />}

      {/* Selection-based actions */}
      <MenuItem onClick={onChangePermissions} disabled={!hasSelection}>
        <ListItemIcon>
          <Icon icon="mdi:shield-lock" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Change Permissions</ListItemText>
      </MenuItem>

      <MenuItem onClick={onCopy} disabled={!hasSelection}>
        <ListItemIcon>
          <Icon icon="mdi:content-copy" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Copy</ListItemText>
      </MenuItem>

      <MenuItem onClick={onCut} disabled={!hasSelection}>
        <ListItemIcon>
          <Icon icon="mdi:content-cut" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Cut</ListItemText>
      </MenuItem>

      <MenuItem onClick={onRename} disabled={renameDisabled}>
        <ListItemIcon>
          <Icon icon="mdi:rename-box" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Rename</ListItemText>
      </MenuItem>

      <MenuItem onClick={onPaste} disabled={!hasClipboard}>
        <ListItemIcon>
          <Icon icon="mdi:content-paste" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Paste</ListItemText>
      </MenuItem>

      <Divider />

      <MenuItem onClick={onDownload} disabled={!hasSelection}>
        <ListItemIcon>
          <Icon icon="mdi:download" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Download</ListItemText>
      </MenuItem>

      <MenuItem onClick={onShowDetails} disabled={detailsDisabled}>
        <ListItemIcon>
          <Icon icon="mdi:eye" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Show Details</ListItemText>
      </MenuItem>

      <MenuItem onClick={onCompress} disabled={compressDisabled}>
        <ListItemIcon>
          <Icon icon="mdi:archive" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Compress to ZIP</ListItemText>
      </MenuItem>

      <MenuItem onClick={onExtract} disabled={extractDisabled}>
        <ListItemIcon>
          <Icon icon="mdi:archive-arrow-up" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Extract Here</ListItemText>
      </MenuItem>

      <MenuItem onClick={onDelete} disabled={!hasSelection}>
        <ListItemIcon>
          <Icon icon="mdi:delete" width={20} height={20} />
        </ListItemIcon>
        <ListItemText>Delete</ListItemText>
      </MenuItem>
    </Menu>
  );
};

export default ContextMenu;
