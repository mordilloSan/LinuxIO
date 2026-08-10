import { createContext, type ReactNode } from "react";
import { type ToastT } from "sonner";

import type { ToastMeta } from "@/types/navigation";

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

const storedToastTypes = [
  "normal",
  "action",
  "success",
  "info",
  "warning",
  "error",
  "loading",
  "default",
] as const satisfies readonly NonNullable<ToastT["type"]>[];

const isRecord = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === "object" && !Array.isArray(value);

const isStoredToastType = (
  value: unknown,
): value is NonNullable<ToastT["type"]> =>
  typeof value === "string" &&
  storedToastTypes.some((toastType) => toastType === value);

const parseStoredHistory = (): ToastHistoryItem[] => {
  if (!isBrowser) return [];
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed
      .flatMap<ToastHistoryItem>((item) => {
        if (!isRecord(item)) return [];

        const id = item.id;
        if (typeof id !== "string" && typeof id !== "number") return [];

        const createdAt = Number(item.createdAt || Date.now());
        if (!createdAt) return [];

        const rawTitle = item.title;
        const title =
          typeof rawTitle === "string" || typeof rawTitle === "number"
            ? String(rawTitle)
            : "Notification";

        return [
          {
            id,
            title,
            description:
              typeof item.description === "string"
                ? item.description || undefined
                : undefined,
            type: isStoredToastType(item.type) ? item.type : undefined,
            createdAt,
            meta: isRecord(item.meta) ? item.meta : undefined,
          },
        ];
      })
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

export const getHistorySnapshot = (): ToastHistoryItem[] =>
  (inMemoryHistory ??= parseStoredHistory());

export const subscribeToHistory = (listener: () => void): (() => void) => {
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

export const setHistoryStore = (next: ToastHistoryItem[]) => {
  inMemoryHistory = next;
  for (const listener of historyListeners) {
    listener();
  }
};

// Fold a sonner change into the merged history. Idempotent (entries are
// keyed by id), so re-runs with the same toasts leave the store untouched.
export const foldSonnerToasts = (toasts: ToastT[]) => {
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

export const clearToastHistory = (toasts: ToastT[]) => {
  toasts.forEach((toastItem) => {
    ignoredToastIds.add(`${sessionId}:${toastItem.id}`);
  });
  persist([]);
  setHistoryStore([]);
};

export interface ToastHistoryContextValue {
  clearHistory: () => void;
  history: ToastHistoryItem[];
}

export const ToastHistoryContext =
  createContext<ToastHistoryContextValue | null>(null);
