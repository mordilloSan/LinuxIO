import type { DragEvent } from "react";
import { toast } from "sonner";

import { INTERNAL_DRAG_TYPE, TRASH_DRAG_TYPE } from "./-dragTypes";

// Drags of the file browser's own items carry them as JSON under their own
// type, with the origin of the page they were dragged from and the token of
// the drag. During dragover only `types` is readable; the payload itself is
// available on drop. The data crosses to any tab of the browser, also to a tab
// of another server's LinuxIO or of any other site, and any page can write
// such a payload, so a drop checks it before it acts (acceptDragFrom).

const itemCount = (count: number) => `${count} item${count === 1 ? "" : "s"}`;

const parsePayload = (dataTransfer: DataTransfer, type: string): unknown => {
  try {
    return JSON.parse(dataTransfer.getData(type));
  } catch {
    return null;
  }
};

const fieldsOf = (payload: unknown): Record<string, unknown> =>
  payload && typeof payload === "object" && !Array.isArray(payload)
    ? (payload as Record<string, unknown>)
    : {};

const stringField = (payload: unknown, key: string) => {
  const value = fieldsOf(payload)[key];
  return typeof value === "string" ? value : "";
};

// The token of the drag that runs, which only this server's own pages know:
// kept in this tab, and in localStorage, which the browser shares between the
// tabs of one origin and with no other site.
const DRAG_TOKEN_KEY = "linuxio.fileBrowserDragToken";
let tabToken: string | null = null;

/** The token stored for the tabs of this origin; undefined without storage. */
const storedToken = (): string | null | undefined => {
  try {
    return window.localStorage.getItem(DRAG_TOKEN_KEY);
  } catch {
    return undefined;
  }
};

/**
 * Starts a drag's token: random, kept until the drag ends at source, or until
 * the next drag replaces it.
 */
const startDragToken = (source?: EventTarget | null): string => {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  const token = Array.from(bytes, (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  tabToken = token;
  try {
    window.localStorage.setItem(DRAG_TOKEN_KEY, token);
  } catch {
    // Without storage only this tab knows the token: drags between tabs are
    // refused, drags inside the tab still work.
  }
  source?.addEventListener(
    "dragend",
    () => {
      if (tabToken === token) tabToken = null;
      try {
        if (window.localStorage.getItem(DRAG_TOKEN_KEY) === token) {
          window.localStorage.removeItem(DRAG_TOKEN_KEY);
        }
      } catch {
        // Nothing was stored.
      }
    },
    { once: true },
  );
  return token;
};

/**
 * Whether a drag of count items came from a page of this server, which the
 * drop may act on. The origin it names tells an honest drag from another
 * server's tab apart, whose paths are that server's; the token tells this
 * server's own drag from one any page can forge with this origin and any
 * path. A drop that fails either is refused with a message, and the drop site
 * sends nothing.
 */
export const acceptDragFrom = (payload: unknown, count: number): boolean => {
  const origin = stringField(payload, "origin");
  const token = stringField(payload, "token");
  const stored = storedToken();
  const fromHere = origin === window.location.origin;
  if (fromHere && token && (token === tabToken || token === stored)) {
    return true;
  }
  const one = count === 1;
  let description: string;
  if (origin && !fromHere) {
    description = `${one ? "It comes" : "They come"} from ${origin}, another server. Drop ${one ? "it" : "them"} in a tab of that server.`;
  } else if (fromHere && token && stored === undefined) {
    description = `This browser keeps LinuxIO from its storage, which a drag between tabs needs. Drop ${one ? "it" : "them"} in the tab ${one ? "it" : "they"} came from.`;
  } else {
    description = `${one ? "It was" : "They were"} not dragged from this LinuxIO, or from a tab of it opened before an update. Reload that tab and drag ${one ? "it" : "them"} again.`;
  }
  toast.error(`Cannot drop ${itemCount(count)} here`, { description });
  return false;
};

/**
 * Whether a drag carries files from the desktop (the browser's own `Files`
 * type). A target that takes none must still prevent the drop's default, or
 * the browser opens a dropped file in LinuxIO's place.
 */
export const hasFileDrag = (dataTransfer: DataTransfer | null) =>
  Boolean(dataTransfer?.types.includes("Files"));

/**
 * For a target that takes no desktop files: keeps the browser from opening
 * one dropped there. Over the target the drag shows as not droppable, and a
 * drop does nothing. The event still reaches an ancestor that takes files,
 * such as the folder's upload, which may then take them.
 */
export const refuseFileDrop = (
  event: Pick<DragEvent, "dataTransfer" | "preventDefault" | "type">,
) => {
  if (!hasFileDrag(event.dataTransfer)) return;
  event.preventDefault();
  if (event.type === "dragover") event.dataTransfer.dropEffect = "none";
};

// Items dragged from a folder, which a drop moves, copies or trashes.

export const hasInternalDrag = (dataTransfer: DataTransfer | null) =>
  Boolean(dataTransfer?.types.includes(INTERNAL_DRAG_TYPE));

/** Starts a drag of items from a folder; source is the element dragged. */
export const setInternalDrag = (
  dataTransfer: DataTransfer,
  paths: string[],
  source?: EventTarget | null,
) => {
  dataTransfer.setData(
    INTERNAL_DRAG_TYPE,
    JSON.stringify({
      origin: window.location.origin,
      paths,
      token: startDragToken(source),
    }),
  );
  dataTransfer.effectAllowed = "copyMove";
};

/**
 * The paths a drop of items dragged from a folder acts on: none when the drag
 * did not come from this server's own page, which is refused (see
 * acceptDragFrom).
 */
export const acceptInternalDrop = (dataTransfer: DataTransfer): string[] => {
  const payload = parsePayload(dataTransfer, INTERNAL_DRAG_TYPE);
  const raw = fieldsOf(payload).paths;
  const paths = Array.isArray(raw)
    ? raw.filter((path): path is string => typeof path === "string")
    : [];
  if (paths.length === 0) return [];
  return acceptDragFrom(payload, paths.length) ? paths : [];
};

/** An item dragged out of the Trash: where it sits there and its shown name. */
export interface TrashDragItem {
  name: string;
  path: string;
}

// A drag out of the Trash carries its items under TRASH_DRAG_TYPE only, so
// the move and upload handlers ignore it and a folder restores it instead.

export const hasTrashDrag = (dataTransfer: DataTransfer | null) =>
  Boolean(dataTransfer?.types.includes(TRASH_DRAG_TYPE));

/** Starts a drag of items out of the Trash; source is the element dragged. */
export const setTrashDrag = (
  dataTransfer: DataTransfer,
  items: readonly TrashDragItem[],
  source?: EventTarget | null,
) => {
  dataTransfer.setData(
    TRASH_DRAG_TYPE,
    JSON.stringify({
      items,
      origin: window.location.origin,
      token: startDragToken(source),
    }),
  );
  dataTransfer.effectAllowed = "move";
};

/**
 * The items a drop of items dragged out of the Trash restores: none when the
 * drag did not come from this server's own Trash, which is refused (see
 * acceptDragFrom).
 */
export const acceptTrashDrop = (
  dataTransfer: DataTransfer,
): TrashDragItem[] => {
  const payload = parsePayload(dataTransfer, TRASH_DRAG_TYPE);
  const rawItems = fieldsOf(payload).items;
  const raw = Array.isArray(rawItems) ? rawItems : [];
  const items = raw.flatMap((item: unknown) => {
    const { name, path } = (item ?? {}) as Partial<TrashDragItem>;
    return typeof path === "string" && path && typeof name === "string"
      ? [{ name, path }]
      : [];
  });
  if (items.length === 0) return [];
  return acceptDragFrom(payload, items.length) ? items : [];
};
