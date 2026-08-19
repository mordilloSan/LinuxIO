import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
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
      {/* The end slot already renders shortcut hints muted and a size down
          from the label, so they need no typography of their own. */}
      <AppMenuItem endAdornment="Ctrl+Shift+C" onClick={onCopy}>
        Copy
      </AppMenuItem>
      <AppMenuItem endAdornment="Ctrl+Shift+V" onClick={onPaste}>
        Paste
      </AppMenuItem>
    </AppMenu>
  );
}

export default TerminalContextMenu;
