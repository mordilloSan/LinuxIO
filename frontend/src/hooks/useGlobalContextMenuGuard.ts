import { useEffect } from "react";

import { targetAllowsContextMenu } from "@/utils/contextMenu";

// Blocks the browser's native context menu app-wide, except inside elements
// marked with allowContextMenuProps (see @/utils/contextMenu).
export function useGlobalContextMenuGuard() {
  useEffect(() => {
    const handleContextMenu = (event: MouseEvent) => {
      if (!targetAllowsContextMenu(event.target)) {
        event.preventDefault();
      }
    };

    document.addEventListener("contextmenu", handleContextMenu);
    return () => {
      document.removeEventListener("contextmenu", handleContextMenu);
    };
  }, []);
}
