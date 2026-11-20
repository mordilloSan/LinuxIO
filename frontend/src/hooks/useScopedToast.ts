import type { ReactNode } from "react";
import { useMemo } from "react";
import { type ExternalToast, toast } from "sonner";

import { type ToastMeta, toastLinkOptions } from "@/api/toast-meta";

type ToastFn = typeof toast.success;
/** Sonner's options, plus a `meta.label` that renames this toast's link. */
type ToastOpts = ExternalToast & { meta?: { label?: string } };

interface ScopedToast {
  error: (msg: ReactNode, opts?: ToastOpts) => ReturnType<ToastFn>;
  info: (msg: ReactNode, opts?: ToastOpts) => ReturnType<ToastFn>;
  success: (msg: ReactNode, opts?: ToastOpts) => ReturnType<ToastFn>;
  warning: (msg: ReactNode, opts?: ToastOpts) => ReturnType<ToastFn>;
}

/**
 * Sonner's toast functions for feedback that belongs to meta's page: a toast
 * fired while the user is on another page offers a button that opens it
 * (toastLinkOptions).
 */
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
      return fn(
        msg,
        toastLinkOptions(createMeta(override?.label ?? label), toastOptions),
      );
    };

    return {
      success: wrap(toast.success),
      error: wrap(toast.error),
      info: wrap(toast.info),
      warning: wrap(toast.warning),
    };
  }, [label, splat, to]);
}
