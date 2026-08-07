import { useContext, useMemo } from "react";

import {
  ToastHistoryContext,
  type ToastHistoryItem,
} from "@/contexts/ToastContext";

export const useToastHistory = (limit = 5): ToastHistoryItem[] => {
  const context = useContext(ToastHistoryContext);

  if (!context) {
    throw new Error("useToastHistory must be used within ToastProvider");
  }

  return useMemo(
    () => context.history.slice(0, limit),
    [context.history, limit],
  );
};
export const useClearToastHistory = (): (() => void) => {
  const context = useContext(ToastHistoryContext);

  if (!context) {
    throw new Error("useClearToastHistory must be used within ToastProvider");
  }

  return context.clearHistory;
};
