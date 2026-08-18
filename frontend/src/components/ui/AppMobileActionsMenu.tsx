import type { ReactNode } from "react";

import AppMenu from "./AppMenu";

interface AppMobileActionsMenuProps {
  anchorEl: HTMLElement | null;
  children: ReactNode;
  onClose: () => void;
  open: boolean;
}

/** The compact icon-row menu shared by small-screen route headers. */
const AppMobileActionsMenu = ({
  anchorEl,
  children,
  onClose,
  open,
}: AppMobileActionsMenuProps) => (
  <AppMenu
    anchorEl={anchorEl}
    anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
    ariaLabel="Actions"
    keepMounted
    minWidth={0}
    onClose={onClose}
    open={open}
    transformOrigin={{ vertical: "top", horizontal: "right" }}
  >
    <div className="app-mobile-actions-menu">{children}</div>
  </AppMenu>
);

export default AppMobileActionsMenu;
