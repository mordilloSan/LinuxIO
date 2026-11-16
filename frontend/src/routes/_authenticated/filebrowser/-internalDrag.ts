import { INTERNAL_DRAG_TYPE, TRASH_DRAG_TYPE } from "./-dragTypes";

// Drags between rows and breadcrumbs inside the browser carry their source
// paths as JSON under INTERNAL_DRAG_TYPE. During dragover only `types` is
// readable; the payload itself is available on drop.

export const hasInternalDrag = (dataTransfer: DataTransfer | null) =>
  Boolean(dataTransfer?.types.includes(INTERNAL_DRAG_TYPE));

export const setInternalDrag = (
  dataTransfer: DataTransfer,
  paths: string[],
) => {
  dataTransfer.setData(INTERNAL_DRAG_TYPE, JSON.stringify(paths));
  dataTransfer.effectAllowed = "copyMove";
};

export const readInternalDrag = (dataTransfer: DataTransfer): string[] => {
  try {
    const parsed: unknown = JSON.parse(
      dataTransfer.getData(INTERNAL_DRAG_TYPE),
    );
    return Array.isArray(parsed)
      ? parsed.filter((path): path is string => typeof path === "string")
      : [];
  } catch {
    return [];
  }
};

/** An item dragged out of the Trash: where it sits there and its shown name. */
export interface TrashDragItem {
  name: string;
  path: string;
}

// A drag out of the Trash carries its items under TRASH_DRAG_TYPE only, so
// the move and upload handlers ignore it and a folder restores it instead.
// The data crosses to another LinuxIO tab like the internal type does.

export const hasTrashDrag = (dataTransfer: DataTransfer | null) =>
  Boolean(dataTransfer?.types.includes(TRASH_DRAG_TYPE));

export const setTrashDrag = (
  dataTransfer: DataTransfer,
  items: readonly TrashDragItem[],
) => {
  dataTransfer.setData(TRASH_DRAG_TYPE, JSON.stringify(items));
  dataTransfer.effectAllowed = "move";
};

export const readTrashDrag = (dataTransfer: DataTransfer): TrashDragItem[] => {
  try {
    const parsed: unknown = JSON.parse(dataTransfer.getData(TRASH_DRAG_TYPE));
    if (!Array.isArray(parsed)) return [];
    return parsed.flatMap((item: unknown) => {
      const { name, path } = (item ?? {}) as Partial<TrashDragItem>;
      return typeof path === "string" && path && typeof name === "string"
        ? [{ name, path }]
        : [];
    });
  } catch {
    return [];
  }
};
