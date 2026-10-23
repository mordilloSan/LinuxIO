import { Icon } from "@iconify/react";

import AppDivider from "@/components/ui/AppDivider";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTypography from "@/components/ui/AppTypography";

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
  onDeletePermanently: () => void;
  onDownload: () => void;
  onExtract?: () => void;
  onOpenContainingFolder?: () => void;
  onPaste: () => void;
  onRename: () => void;
  onShowDetails?: () => void;
  onUpload: () => void;
}

const shortcutHint = (keys: string) => (
  <AppTypography color="text.secondary" variant="caption">
    {keys}
  </AppTypography>
);

const ContextMenu = ({
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
  onDeletePermanently,
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
}: ContextMenuProps) => {
  const detailsDisabled =
    canShowDetails === undefined ? !hasSelection : !canShowDetails;
  const compressDisabled =
    canCompress === undefined ? !hasSelection : !canCompress;
  const extractDisabled = canExtract === undefined ? true : !canExtract;
  const renameDisabled = canRename === undefined ? !hasSelection : !canRename;

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
      {/* Always available actions. Glyph sizing is app-menu.css's, which
          normalises it across every menu, so call sites only name the icon. */}
      <AppMenuItem
        onClick={onCreateFile}
        startAdornment={<Icon icon="mdi:file-plus" />}
      >
        Create File
      </AppMenuItem>

      <AppMenuItem
        onClick={onCreateFolder}
        startAdornment={<Icon icon="mdi:folder-plus" />}
      >
        New folder
      </AppMenuItem>

      <AppMenuItem
        onClick={onUpload}
        startAdornment={<Icon icon="mdi:upload" />}
      >
        Upload
      </AppMenuItem>

      <AppDivider />

      {/* Open containing folder (search results only) */}
      {canOpenContainingFolder && (
        <AppMenuItem
          onClick={onOpenContainingFolder}
          startAdornment={<Icon icon="mdi:folder-open" />}
        >
          Open Containing Folder
        </AppMenuItem>
      )}

      {canOpenContainingFolder && <AppDivider />}

      {/* Selection-based actions */}
      <AppMenuItem
        disabled={!hasSelection}
        onClick={onChangePermissions}
        startAdornment={<Icon icon="mdi:shield-lock" />}
      >
        Change Permissions
      </AppMenuItem>

      <AppMenuItem
        disabled={!hasSelection}
        endAdornment={shortcutHint("Ctrl+C")}
        onClick={onCopy}
        startAdornment={<Icon icon="mdi:content-copy" />}
      >
        Copy
      </AppMenuItem>

      <AppMenuItem
        disabled={!hasSelection}
        endAdornment={shortcutHint("Ctrl+X")}
        onClick={onCut}
        startAdornment={<Icon icon="mdi:content-cut" />}
      >
        Cut
      </AppMenuItem>

      <AppMenuItem
        disabled={renameDisabled}
        endAdornment={shortcutHint("F2")}
        onClick={onRename}
        startAdornment={<Icon icon="mdi:rename-box" />}
      >
        Rename
      </AppMenuItem>

      <AppMenuItem
        disabled={!hasClipboard}
        endAdornment={shortcutHint("Ctrl+V")}
        onClick={onPaste}
        startAdornment={<Icon icon="mdi:content-paste" />}
      >
        Paste
      </AppMenuItem>

      <AppDivider />

      <AppMenuItem
        disabled={!hasSelection}
        onClick={onDownload}
        startAdornment={<Icon icon="mdi:download" />}
      >
        Download
      </AppMenuItem>

      <AppMenuItem
        disabled={detailsDisabled}
        onClick={onShowDetails}
        startAdornment={<Icon icon="mdi:eye" />}
      >
        Show Details
      </AppMenuItem>

      <AppMenuItem
        disabled={compressDisabled}
        onClick={onCompress}
        startAdornment={<Icon icon="mdi:archive" />}
      >
        Compress
      </AppMenuItem>

      <AppMenuItem
        disabled={extractDisabled}
        onClick={onExtract}
        startAdornment={<Icon icon="mdi:archive-arrow-up" />}
      >
        Extract to new folder
      </AppMenuItem>

      <AppDivider />

      <AppMenuItem
        disabled={!hasSelection}
        endAdornment={shortcutHint("Del")}
        onClick={onDelete}
        startAdornment={<Icon icon="mdi:delete" />}
      >
        Delete
      </AppMenuItem>

      <AppMenuItem
        danger
        disabled={!hasSelection}
        endAdornment={shortcutHint("Shift+Del")}
        onClick={onDeletePermanently}
        startAdornment={<Icon icon="mdi:delete-forever" />}
      >
        Delete permanently
      </AppMenuItem>
    </AppMenu>
  );
};

export default ContextMenu;
