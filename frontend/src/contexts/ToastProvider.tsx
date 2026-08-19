import {
  useCallback,
  useEffect,
  useMemo,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { toast, Toaster, type ToastT, useSonner } from "sonner";

import "@/contexts/app-toast.css";
import {
  foldSonnerToasts,
  clearToastHistory,
  getHistorySnapshot,
  subscribeToHistory,
  ToastHistoryContext,
} from "@/contexts/ToastContext";

export const ToastProvider = ({ children }: { children: ReactNode }) => {
  const { toasts } = useSonner();
  const history = useSyncExternalStore(subscribeToHistory, getHistorySnapshot);

  useEffect(() => {
    foldSonnerToasts(toasts);
  }, [toasts]);

  const clearHistory = useCallback(() => {
    const activeToasts = toast
      .getHistory()
      .filter((item): item is ToastT => !("dismiss" in item));
    clearToastHistory(activeToasts);
    toast.dismiss();
  }, []);

  const contextValue = useMemo(
    () => ({ history, clearHistory }),
    [history, clearHistory],
  );

  return (
    <ToastHistoryContext.Provider value={contextValue}>
      {children}
      {/* Chrome lives in app-toast.css, which paints the floating-surface
          look over sonner's custom properties — hence no `richColors`: its
          filled per-type panels would overwrite that surface. */}
      <Toaster
        className="app-toaster"
        position="top-right"
        toastOptions={{ duration: 1500 }}
      />
    </ToastHistoryContext.Provider>
  );
};
