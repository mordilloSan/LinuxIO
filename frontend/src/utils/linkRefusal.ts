import type { UseNavigateResult } from "@tanstack/react-router";
import type { toast as sonnerToast } from "sonner";

// Sonner's own options type, taken from its error toast.
type ToastOpts = NonNullable<Parameters<typeof sonnerToast.error>[1]>;

// The bridge refuses a change that would go through a symbolic link someone
// other than root could have planted, and names where the link points
// (plantedLinkError in backend/bridge/internal/linksafe/linksafe.go).
// Errors carry only a message, so the destination is found by that wording;
// linkRefusal.test.ts fails when the Go text stops matching.
const LINK_REFUSAL =
  / is a link to (\/.*?)\. Open that folder to make this change/;

/** A toast with an action has to stay long enough to be clicked. */
const LINK_REFUSAL_TOAST_MS = 10_000;

/**
 * The absolute path a refused link points to, from a server error message or
 * from batch failure lines; null when the text holds no link refusal. Of
 * several refusals the first one counts.
 */
export function linkRefusalDestination(text: string): string | null {
  return LINK_REFUSAL.exec(text)?.[1] ?? null;
}

const LINK_REFUSAL_PARTS =
  /(?:^|: )(\/.*?) is a link to (\/.*?)\. Open that folder to make this change/;

/**
 * Where path leads once the link a refusal names is replaced by what it
 * points to: the refused link is path itself or a folder on the way to it,
 * and what lies below the link is kept. Null when the text holds no link
 * refusal. `link` is the link as the server named it.
 */
export function pathPastRefusedLink(
  path: string,
  text: string,
): { link: string; path: string } | null {
  const match = LINK_REFUSAL_PARTS.exec(text);
  if (!match) return null;
  const [, link, destination] = match;
  const below = path === link || path.startsWith(`${link}/`);
  const rest = below ? path.slice(link.length) : "";
  return {
    link,
    path: destination === "/" ? rest || "/" : `${destination}${rest}`,
  };
}

interface ErrorToaster {
  error: (message: string, opts?: ToastOpts) => unknown;
}

/**
 * Raises an error toast for a file operation. When the message or its
 * description holds a link refusal, the toast offers to open the folder the
 * link points to. `onOpen` runs right before it navigates, for a caller whose
 * dialog would otherwise stay open over the folder it opens.
 */
export function toastFileError(
  toast: ErrorToaster,
  navigate: UseNavigateResult<string>,
  message: string,
  opts?: ToastOpts,
  onOpen?: () => void,
): void {
  const description = opts?.description;
  const destination =
    linkRefusalDestination(message) ??
    (typeof description === "string"
      ? linkRefusalDestination(description)
      : null);
  if (destination === null) {
    // Without options the call stays a one-argument one, as before.
    if (opts) toast.error(message, opts);
    else toast.error(message);
    return;
  }
  toast.error(message, {
    ...opts,
    action: {
      label: "Open the real folder",
      onClick: () => {
        onOpen?.();
        void navigate({
          to: "/filebrowser/$",
          params: { _splat: destination.split("/").filter(Boolean).join("/") },
          search: {},
        });
      },
    },
    duration: LINK_REFUSAL_TOAST_MS,
  });
}
