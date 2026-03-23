import { Icon } from "@iconify/react";
import React, { useEffect, useEffectEvent } from "react";

import AppDivider from "@/components/ui/AppDivider";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";

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
      open={Boolean(anchorPosition)}
      onClose={onClose}
      anchorPosition={
        anchorPosition
          ? { top: anchorPosition.top, left: anchorPosition.left }
          : undefined
      }
      minWidth={200}
    >
      {/* Always available actions */}
      <AppMenuItem
        onClick={onCreateFile}
        startAdornment={<Icon icon="mdi:file-plus" width={20} height={20} />}
      >
        Create File
      </AppMenuItem>

      <AppMenuItem
        onClick={onCreateFolder}
        startAdornment={<Icon icon="mdi:folder-plus" width={20} height={20} />}
      >
        Create Folder
      </AppMenuItem>

      <AppMenuItem
        onClick={onUpload}
        startAdornment={<Icon icon="mdi:upload" width={20} height={20} />}
      >
        Upload
      </AppMenuItem>

      <AppDivider />

      {/* Open containing folder (search results only) */}
      {canOpenContainingFolder && (
        <AppMenuItem
          onClick={onOpenContainingFolder}
          startAdornment={
            <Icon icon="mdi:folder-open" width={20} height={20} />
          }
        >
          Open Containing Folder
        </AppMenuItem>
      )}

      {canOpenContainingFolder && <AppDivider />}

      {/* Selection-based actions */}
      <AppMenuItem
        onClick={onChangePermissions}
        disabled={!hasSelection}
        startAdornment={<Icon icon="mdi:shield-lock" width={20} height={20} />}
      >
        Change Permissions
      </AppMenuItem>

      <AppMenuItem
        onClick={onCopy}
        disabled={!hasSelection}
        startAdornment={<Icon icon="mdi:content-copy" width={20} height={20} />}
      >
        Copy
      </AppMenuItem>

      <AppMenuItem
        onClick={onCut}
        disabled={!hasSelection}
        startAdornment={<Icon icon="mdi:content-cut" width={20} height={20} />}
      >
        Cut
      </AppMenuItem>

      <AppMenuItem
        onClick={onRename}
        disabled={renameDisabled}
        startAdornment={<Icon icon="mdi:rename-box" width={20} height={20} />}
      >
        Rename
      </AppMenuItem>

      <AppMenuItem
        onClick={onPaste}
        disabled={!hasClipboard}
        startAdornment={
          <Icon icon="mdi:content-paste" width={20} height={20} />
        }
      >
        Paste
      </AppMenuItem>

      <AppDivider />

      <AppMenuItem
        onClick={onDownload}
        disabled={!hasSelection}
        startAdornment={<Icon icon="mdi:download" width={20} height={20} />}
      >
        Download
      </AppMenuItem>

      <AppMenuItem
        onClick={onShowDetails}
        disabled={detailsDisabled}
        startAdornment={<Icon icon="mdi:eye" width={20} height={20} />}
      >
        Show Details
      </AppMenuItem>

      <AppMenuItem
        onClick={onCompress}
        disabled={compressDisabled}
        startAdornment={<Icon icon="mdi:archive" width={20} height={20} />}
      >
        Compress to ZIP
      </AppMenuItem>

      <AppMenuItem
        onClick={onExtract}
        disabled={extractDisabled}
        startAdornment={
          <Icon icon="mdi:archive-arrow-up" width={20} height={20} />
        }
      >
        Extract Here
      </AppMenuItem>

      <AppMenuItem
        onClick={onDelete}
        disabled={!hasSelection}
        startAdornment={<Icon icon="mdi:delete" width={20} height={20} />}
      >
        Delete
      </AppMenuItem>
    </AppMenu>
  );
};

export default ContextMenu;
