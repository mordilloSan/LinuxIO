import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTypography from "@/components/ui/AppTypography";
import type { TerminalContextMenuPosition } from "@/hooks/useTerminalContextMenu";

interface TerminalContextMenuProps {
  contextMenu: TerminalContextMenuPosition | null;
  onClose: () => void;
  onCopy: () => void;
  onPaste: () => void;
}

function TerminalContextMenu({
  contextMenu,
  onClose,
  onCopy,
  onPaste,
}: TerminalContextMenuProps) {
  return (
    <AppMenu
      anchorPosition={
        contextMenu !== null
          ? { top: contextMenu.mouseY, left: contextMenu.mouseX }
          : undefined
      }
      autoFocus={false}
      minWidth={168}
      onClose={onClose}
      open={contextMenu !== null}
    >
      <AppMenuItem
        endAdornment={
          <AppTypography
            color="text.secondary"
            style={{ marginLeft: 8 }}
            variant="body2"
          >
            Ctrl+Shift+C
          </AppTypography>
        }
        onClick={onCopy}
      >
        Copy
      </AppMenuItem>
      <AppMenuItem
        endAdornment={
          <AppTypography
            color="text.secondary"
            style={{ marginLeft: 8 }}
            variant="body2"
          >
            Ctrl+Shift+V
          </AppTypography>
        }
        onClick={onPaste}
      >
        Paste
      </AppMenuItem>
    </AppMenu>
  );
}

export default TerminalContextMenu;
