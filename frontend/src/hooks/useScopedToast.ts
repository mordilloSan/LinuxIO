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
  return useMemo(() => {
    const wrap = (fn: ToastFn) => (msg: ReactNode, opts?: ToastOpts) =>
      fn(msg, { ...opts, meta: { ...meta, ...(opts?.meta ?? {}) } });
    return {
      success: wrap(toast.success),
      error: wrap(toast.error),
      info: wrap(toast.info),
      warning: wrap(toast.warning),
    };
  }, [meta.href, meta.label]);
}
