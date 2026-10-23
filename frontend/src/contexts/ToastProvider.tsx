import type { ReactNode } from "react";
import { Toaster } from "sonner";

import "./ToastProvider.css";

export const ToastProvider = ({ children }: { children: ReactNode }) => (
  <>
    {children}
    {/* Chrome lives in ToastProvider.css, which paints the floating-surface
        look over sonner's custom properties — hence no `richColors`: its
        filled per-type panels would overwrite that surface. */}
    <Toaster
      className="app-toaster"
      position="top-right"
      toastOptions={{ duration: 1500 }}
    />
  </>
);
