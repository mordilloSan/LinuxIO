import React, { createContext, useState, useEffect, useCallback } from "react";
import { toast, useSonner, Toaster, type ToastT } from "sonner";

export type ToastMeta = {
  href?: string;
  label?: string;
};

export type ToastHistoryItem = {
  id: string | number;
  title: string;
  description?: string;
  type?: ToastT["type"];
  createdAt: number;
  meta?: ToastMeta;
};

const STORAGE_KEY = "linuxio.toastHistory";
const MAX_STORED_TOASTS = 50;

const isBrowser = typeof window !== "undefined";
const sessionId = `${Date.now().toString(36)}-${
  isBrowser
    ? Array.from(crypto.getRandomValues(new Uint8Array(4)))
        .map((b) => b.toString(16).padStart(2, "0"))
        .join("")
    : Math.random().toString(36).slice(2, 8)
}`;
const ignoredToastIds = new Set<string>();

const parseStoredHistory = (): ToastHistoryItem[] => {
  if (!isBrowser) return [];
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((item) => item && typeof item === "object")
      .map((item) => {
        const rawTitle = (item as ToastHistoryItem).title;
        return {
          id: (item as ToastHistoryItem).id,
          title:
            typeof rawTitle === "string" || typeof rawTitle === "number"
              ? String(rawTitle)
              : "Notification",
          description: (item as ToastHistoryItem).description || undefined,
          type: (item as ToastHistoryItem).type,
          createdAt: Number((item as ToastHistoryItem).createdAt || Date.now()),
          meta: (item as ToastHistoryItem).meta,
        };
      })
      .filter((item) => item.id !== undefined && item.title && item.createdAt)
      .slice(0, MAX_STORED_TOASTS);
  } catch {
    return [];
  }
};

const persist = (history: ToastHistoryItem[]) => {
  if (!isBrowser) return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(history));
  } catch {
    // ignore storage failures
  }
};

const coerceText = (
  node?: React.ReactNode | (() => React.ReactNode),
): string => {
  if (typeof node === "function") {
    return coerceText(node());
  }
  if (typeof node === "string" || typeof node === "number") {
    return String(node).trim();
  }
  if (Array.isArray(node)) {
    return node
      .map((part) => coerceText(part))
      .filter(Boolean)
      .join(" ")
      .trim();
  }
  return "";
};

const buildHistorySnapshot = (
  currentHistory: ToastHistoryItem[],
  toasts: ToastT[],
): ToastHistoryItem[] => {
  const now = Date.now();
  const existingById = new Map(currentHistory.map((item) => [item.id, item]));
  const fromSonner = toasts.filter(
    (item): item is ToastT => !("dismiss" in item),
  );
  const nextFromSonner = fromSonner.reduce<ToastHistoryItem[]>(
    (acc, toastItem, index) => {
      const recordId = `${sessionId}:${toastItem.id}`;
      if (ignoredToastIds.has(recordId)) {
        return acc;
      }
      const existing = existingById.get(recordId);
      const title =
        coerceText(toastItem.title) || existing?.title || "Notification";
      const description = coerceText(toastItem.description) || undefined;
      acc.push({
        id: recordId,
        title,
        description: description || existing?.description,
        type: toastItem.type ?? existing?.type,
        createdAt: existing?.createdAt ?? now + index,
        meta: toastItem.meta ?? existing?.meta,
      });
      return acc;
    },
    [],
  );
  const nextIds = new Set(nextFromSonner.map((item) => item.id));
  const carryOver = currentHistory.filter((item) => !nextIds.has(item.id));
  const merged = [...nextFromSonner, ...carryOver]
    .sort((a, b) => b.createdAt - a.createdAt)
    .slice(0, MAX_STORED_TOASTS);
  return merged;
};

export interface ToastHistoryContextValue {
  history: ToastHistoryItem[];
  clearHistory: () => void;
}

export const ToastHistoryContext =
  createContext<ToastHistoryContextValue | null>(null);

export const ToastProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [history, setHistory] = useState<ToastHistoryItem[]>(() =>
    parseStoredHistory(),
  );
  const { toasts } = useSonner();

  // Sync toast history when toasts change
  useEffect(() => {
    const nextHistory = buildHistorySnapshot(history, toasts);
    setHistory(nextHistory);
    persist(nextHistory);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [toasts]);

  const clearHistory = useCallback(() => {
    const activeToasts = toast
      .getHistory()
      .filter((item): item is ToastT => !("dismiss" in item));
    activeToasts.forEach((toastItem) => {
      ignoredToastIds.add(`${sessionId}:${toastItem.id}`);
    });
    setHistory([]);
    persist([]);
    toast.dismiss();
  }, []);

  return (
    <ToastHistoryContext.Provider value={{ history, clearHistory }}>
      {children}
      <Toaster
        richColors
        position="top-right"
        toastOptions={{ duration: 1500 }}
      />
    </ToastHistoryContext.Provider>
  );
};
