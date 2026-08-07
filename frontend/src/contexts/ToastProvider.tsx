import {
  useCallback,
  useEffect,
  useMemo,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { toast, Toaster, type ToastT, useSonner } from "sonner";

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
      <Toaster
        position="top-right"
        richColors
        toastOptions={{ duration: 1500 }}
      />
    </ToastHistoryContext.Provider>
  );
};
