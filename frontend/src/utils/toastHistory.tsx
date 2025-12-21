import React from "react";
import { toast, useSonner, type ToastT } from "sonner";

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
const sessionId = `${Date.now().toString(36)}-${Math.random()
  .toString(36)
  .slice(2, 8)}`;
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

let historyCache: ToastHistoryItem[] = parseStoredHistory();
const listeners = new Set<(history: ToastHistoryItem[]) => void>();

const notify = () => {
  const snapshot = historyCache.slice();
  listeners.forEach((listener) => listener(snapshot));
};

const persist = () => {
  if (!isBrowser) return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(historyCache));
  } catch {
    // ignore storage failures
  }
};

const setHistory = (next: ToastHistoryItem[]) => {
  historyCache = next;
  persist();
  notify();
};

export const subscribeToastHistory = (
  listener: (history: ToastHistoryItem[]) => void,
): (() => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const useToastHistory = (limit = 5) => {
  const [items, setItems] = React.useState(() => historyCache.slice(0, limit));

  React.useEffect(() => {
    const unsubscribe = subscribeToastHistory((next) => {
      setItems(next.slice(0, limit));
    });
    return unsubscribe;
  }, [limit]);

  return items;
};

export const clearToastHistory = () => {
  const activeToasts = toast
    .getHistory()
    .filter((item): item is ToastT => !("dismiss" in item));
  activeToasts.forEach((toastItem) => {
    ignoredToastIds.add(`${sessionId}:${toastItem.id}`);
  });
  setHistory([]);
  toast.dismiss();
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

const buildHistorySnapshot = () => {
  const now = Date.now();
  const existingById = new Map(historyCache.map((item) => [item.id, item]));
  const fromSonner = toast
    .getHistory()
    .filter((item): item is ToastT => !("dismiss" in item));
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
  const carryOver = historyCache.filter((item) => !nextIds.has(item.id));
  const merged = [...nextFromSonner, ...carryOver]
    .sort((a, b) => b.createdAt - a.createdAt)
    .slice(0, MAX_STORED_TOASTS);
  return merged;
};

export function ToastHistorySync() {
  const { toasts } = useSonner();

  React.useEffect(() => {
    setHistory(buildHistorySnapshot());
  }, [toasts]);

  return null;
}
