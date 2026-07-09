import type { ReactNode } from "react";
import { useMemo } from "react";
import { toast } from "sonner";

import type { ToastMeta } from "@/contexts/ToastContext";

type ToastFn = typeof toast.success;
type ToastOpts = Parameters<ToastFn>[1];

export interface ScopedToast {
  error: (msg: ReactNode, opts?: ToastOpts) => ReturnType<ToastFn>;
  info: (msg: ReactNode, opts?: ToastOpts) => ReturnType<ToastFn>;
  success: (msg: ReactNode, opts?: ToastOpts) => ReturnType<ToastFn>;
  warning: (msg: ReactNode, opts?: ToastOpts) => ReturnType<ToastFn>;
}

export function useScopedToast(meta: ToastMeta): ScopedToast {
  // Destructure before the memo so the closure only reads primitives: callers
  // pass inline meta objects, and depending on `meta` itself would rebuild the
  // toast scope (and re-fire effects that depend on it) every render.
  const { href, label } = meta;
  return useMemo(() => {
    const wrap = (fn: ToastFn) => (msg: ReactNode, opts?: ToastOpts) =>
      fn(msg, { ...opts, meta: { href, label, ...(opts?.meta ?? {}) } });
    return {
      success: wrap(toast.success),
      error: wrap(toast.error),
      info: wrap(toast.info),
      warning: wrap(toast.warning),
    };
  }, [href, label]);
}
