import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { Toaster } from "sonner";

import "./ToastProvider.css";

export const ToastProvider = ({ children }: { children: ReactNode }) => (
  <>
    {children}
    {/* Chrome lives in ToastProvider.css, which paints the floating-surface
        look over sonner's custom properties — hence no `richColors`: its
        filled per-type panels would overwrite that surface. */}
    {/* Mounted on <body>, outside #root: an open dialog blurs #root
        (app-dialog.css), and that filter also keeps everything inside #root
        below the dialog's backdrop, whatever its z-index. */}
    {createPortal(
      <Toaster
        className="app-toaster"
        position="top-right"
        toastOptions={{ duration: 1500 }}
      />,
      document.body,
    )}
  </>
);
