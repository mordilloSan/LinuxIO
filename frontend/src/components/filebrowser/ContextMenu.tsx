import { Icon } from "@iconify/react";
import React, { useEffect, useEffectEvent } from "react";

import AppDivider from "@/components/ui/AppDivider";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";

interface ContextMenuProps {
  anchorPosition: { top: number; left: number } | null;
  canCompress?: boolean;
  canExtract?: boolean;
  canOpenContainingFolder?: boolean;
  canRename?: boolean;
  canShowDetails?: boolean;
  hasClipboard?: boolean;
  hasSelection: boolean;
  onChangePermissions: () => void;
  onClose: () => void;
  onCompress?: () => void;
  onCopy: () => void;
  onCreateFile: () => void;
  onCreateFolder: () => void;
  onCut: () => void;
  onDelete: () => void;
  onDownload: () => void;
  onExtract?: () => void;
  onOpenContainingFolder?: () => void;
  onPaste: () => void;
  onRename: () => void;
  onShowDetails?: () => void;
  onUpload: () => void;
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
    <AppMenu
      anchorPosition={
        anchorPosition
          ? { top: anchorPosition.top, left: anchorPosition.left }
          : undefined
      }
      minWidth={200}
      onClose={onClose}
      open={Boolean(anchorPosition)}
    >
      {/* Always available actions */}
      <AppMenuItem
        onClick={onCreateFile}
        startAdornment={<Icon height={20} icon="mdi:file-plus" width={20} />}
      >
        Create File
      </AppMenuItem>

      <AppMenuItem
        onClick={onCreateFolder}
        startAdornment={<Icon height={20} icon="mdi:folder-plus" width={20} />}
      >
        Create Folder
      </AppMenuItem>

      <AppMenuItem
        onClick={onUpload}
        startAdornment={<Icon height={20} icon="mdi:upload" width={20} />}
      >
        Upload
      </AppMenuItem>

      <AppDivider />

      {/* Open containing folder (search results only) */}
      {canOpenContainingFolder && (
        <AppMenuItem
          onClick={onOpenContainingFolder}
          startAdornment={
            <Icon height={20} icon="mdi:folder-open" width={20} />
          }
        >
          Open Containing Folder
        </AppMenuItem>
      )}

      {canOpenContainingFolder && <AppDivider />}

      {/* Selection-based actions */}
      <AppMenuItem
        disabled={!hasSelection}
        onClick={onChangePermissions}
        startAdornment={<Icon height={20} icon="mdi:shield-lock" width={20} />}
      >
        Change Permissions
      </AppMenuItem>

      <AppMenuItem
        disabled={!hasSelection}
        onClick={onCopy}
        startAdornment={<Icon height={20} icon="mdi:content-copy" width={20} />}
      >
        Copy
      </AppMenuItem>

      <AppMenuItem
        disabled={!hasSelection}
        onClick={onCut}
        startAdornment={<Icon height={20} icon="mdi:content-cut" width={20} />}
      >
        Cut
      </AppMenuItem>

      <AppMenuItem
        disabled={renameDisabled}
        onClick={onRename}
        startAdornment={<Icon height={20} icon="mdi:rename-box" width={20} />}
      >
        Rename
      </AppMenuItem>

      <AppMenuItem
        disabled={!hasClipboard}
        onClick={onPaste}
        startAdornment={
          <Icon height={20} icon="mdi:content-paste" width={20} />
        }
      >
        Paste
      </AppMenuItem>

      <AppDivider />

      <AppMenuItem
        disabled={!hasSelection}
        onClick={onDownload}
        startAdornment={<Icon height={20} icon="mdi:download" width={20} />}
      >
        Download
      </AppMenuItem>

      <AppMenuItem
        disabled={detailsDisabled}
        onClick={onShowDetails}
        startAdornment={<Icon height={20} icon="mdi:eye" width={20} />}
      >
        Show Details
      </AppMenuItem>

      <AppMenuItem
        disabled={compressDisabled}
        onClick={onCompress}
        startAdornment={<Icon height={20} icon="mdi:archive" width={20} />}
      >
        Compress
      </AppMenuItem>

      <AppMenuItem
        disabled={extractDisabled}
        onClick={onExtract}
        startAdornment={
          <Icon height={20} icon="mdi:archive-arrow-up" width={20} />
        }
      >
        Extract Here
      </AppMenuItem>

      <AppMenuItem
        disabled={!hasSelection}
        onClick={onDelete}
        startAdornment={<Icon height={20} icon="mdi:delete" width={20} />}
      >
        Delete
      </AppMenuItem>
    </AppMenu>
  );
};

export default ContextMenu;
