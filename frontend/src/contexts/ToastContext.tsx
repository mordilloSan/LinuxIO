import {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { toast, Toaster, type ToastT, useSonner } from "sonner";

import type { ToastMeta } from "@/routes/-navigation";

export interface ToastHistoryItem {
  createdAt: number;
  description?: string;
  id: string | number;
  meta?: ToastMeta;
  title: string;
  type?: ToastT["type"];
}

const STORAGE_KEY = "linuxio.toastHistory";
const MAX_STORED_TOASTS = 50;
const PERSIST_DEBOUNCE_MS = 1_000;

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

const coerceText = (node?: ReactNode | (() => ReactNode)): string => {
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

// --- history store ----------------------------------------------------------
// Toast history is external state: it is shared with localStorage and fed by
// sonner, and it must survive across renders without living in React state
// (deriving it in render or an effect trips the compiler lint rules). So it
// lives in a tiny module store — effects push sonner changes in, React reads
// it back through useSyncExternalStore. localStorage is parsed once (lazy)
// and only written back, debounced, with a pagehide flush.

let inMemoryHistory: ToastHistoryItem[] | null = null;
let persistTimer: number | undefined;
const historyListeners = new Set<() => void>();

const getHistorySnapshot = (): ToastHistoryItem[] =>
  (inMemoryHistory ??= parseStoredHistory());

const subscribeToHistory = (listener: () => void): (() => void) => {
  historyListeners.add(listener);
  return () => {
    historyListeners.delete(listener);
  };
};

const flushPersist = () => {
  if (persistTimer === undefined) return;
  window.clearTimeout(persistTimer);
  persistTimer = undefined;
  persist(getHistorySnapshot());
};

if (isBrowser) {
  window.addEventListener("pagehide", flushPersist);
}

const schedulePersist = () => {
  if (!isBrowser) return;
  window.clearTimeout(persistTimer);
  persistTimer = window.setTimeout(() => {
    persistTimer = undefined;
    persist(getHistorySnapshot());
  }, PERSIST_DEBOUNCE_MS);
};

const sameHistory = (a: ToastHistoryItem[], b: ToastHistoryItem[]): boolean =>
  a.length === b.length &&
  a.every((item, index) => {
    const other = b[index];
    return (
      item.id === other.id &&
      item.title === other.title &&
      item.description === other.description &&
      item.type === other.type &&
      item.createdAt === other.createdAt &&
      item.meta === other.meta
    );
  });

const setHistoryStore = (next: ToastHistoryItem[]) => {
  inMemoryHistory = next;
  for (const listener of historyListeners) {
    listener();
  }
};

// Fold a sonner change into the merged history. Idempotent (entries are
// keyed by id), so re-runs with the same toasts leave the store untouched.
const foldSonnerToasts = (toasts: ToastT[]) => {
  const current = getHistorySnapshot();
  const next = buildHistorySnapshot(current, toasts);
  if (sameHistory(current, next)) return;
  setHistoryStore(next);
  schedulePersist();
};

// Test-only: drop the in-memory store so each test re-reads localStorage.
export const __resetToastHistoryStore = () => {
  inMemoryHistory = null;
  if (isBrowser) window.clearTimeout(persistTimer);
  persistTimer = undefined;
};

export interface ToastHistoryContextValue {
  clearHistory: () => void;
  history: ToastHistoryItem[];
}

export const ToastHistoryContext =
  createContext<ToastHistoryContextValue | null>(null);

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
    activeToasts.forEach((toastItem) => {
      ignoredToastIds.add(`${sessionId}:${toastItem.id}`);
    });
    persist([]);
    setHistoryStore([]);
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
