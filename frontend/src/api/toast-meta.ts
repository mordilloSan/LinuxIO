import type { ExternalToast } from "sonner";

import type { FileRouteTypes } from "@/routeTree.gen";

export type RouteTarget =
  | {
      params?: never;
      to: Exclude<FileRouteTypes["to"], "/filebrowser/$">;
    }
  | {
      params: { _splat: string };
      to: "/filebrowser/$";
    };

export type ToastMeta =
  | (RouteTarget & {
      label?: string;
    })
  | {
      label?: string;
      params?: never;
      to?: never;
    };

/**
 * What a toast link needs from the app router: the page the user is on now,
 * and a way to open another. router/router.tsx registers the app router here,
 * because `@/api` cannot import it: its route tree imports `@/api`.
 */
export interface ToastLinkRouter {
  navigate: (target: RouteTarget) => void;
  pathname: () => string;
}

let linkRouter: ToastLinkRouter | null = null;

/** Makes toast links use router; the returned release undoes it. */
export function registerToastLinkRouter(router: ToastLinkRouter): () => void {
  linkRouter = router;
  return () => {
    if (linkRouter === router) linkRouter = null;
  };
}

/** A toast with a button has to stay long enough to be clicked. */
const TOAST_LINK_MS = 10_000;

const FILE_BROWSER = "/filebrowser";

/** No page can be opened from here until the user signs in again. */
const SIGN_IN = "/sign-in";

const folderOf = (splat: string) => splat.split("/").filter(Boolean).join("/");

/** The router keeps a few characters of a path encoded, such as `%25`. */
function decoded(path: string): string {
  try {
    return decodeURIComponent(path);
  } catch {
    return path;
  }
}

/**
 * Whether pathname already shows target: the page itself or a page below it,
 * or for the file browser the target folder (an empty splat stands for the
 * file browser as a whole, so any folder in it counts).
 */
function isShowing(pathname: string, target: RouteTarget): boolean {
  if (target.to === "/filebrowser/$") {
    if (pathname !== FILE_BROWSER && !pathname.startsWith(`${FILE_BROWSER}/`)) {
      return false;
    }
    const folder = folderOf(target.params._splat);
    return (
      folder === "" ||
      folder === folderOf(decoded(pathname.slice(FILE_BROWSER.length)))
    );
  }
  return pathname === target.to || pathname.startsWith(`${target.to}/`);
}

/**
 * Sonner options for a toast that belongs to meta's page: opts, plus a button
 * that opens the page when the user is somewhere else as the toast fires.
 * Call it when the toast fires, not before, since the user may have moved on
 * since the work started. An action in opts is kept and never replaced; a meta
 * without a page, or a toast fired on that page, gets opts unchanged.
 */
export function toastLinkOptions(
  meta: ToastMeta | undefined,
  opts?: ExternalToast,
): ExternalToast | undefined {
  const router = linkRouter;
  if (!router || meta?.to === undefined || opts?.action !== undefined) {
    return opts;
  }
  const pathname = router.pathname();
  if (pathname === SIGN_IN || isShowing(pathname, meta)) return opts;
  return {
    duration: TOAST_LINK_MS,
    ...opts,
    action: {
      label: meta.label || "Open",
      onClick: () => router.navigate(meta),
    },
  };
}
