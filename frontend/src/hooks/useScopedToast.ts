import type { ReactNode } from "react";
import { useMemo } from "react";
import { toast } from "sonner";

import type { ToastMeta } from "@/types/navigation";

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
  const { label, to } = meta;
  const splat = meta.to === "/filebrowser/$" ? meta.params._splat : undefined;

  return useMemo(() => {
    const createMeta = (nextLabel?: string): ToastMeta => {
      if (to === "/filebrowser/$") {
        return {
          label: nextLabel,
          params: { _splat: splat ?? "" },
          to,
        };
      }
      if (to) return { label: nextLabel, to };
      return { label: nextLabel };
    };
    const wrap = (fn: ToastFn) => (msg: ReactNode, opts?: ToastOpts) => {
      const { meta: override, ...toastOptions } = opts ?? {};
      return fn(msg, {
        ...toastOptions,
        meta: createMeta(override?.label ?? label),
      });
    };

    return {
      success: wrap(toast.success),
      error: wrap(toast.error),
      info: wrap(toast.info),
      warning: wrap(toast.warning),
    };
  }, [label, splat, to]);
}
