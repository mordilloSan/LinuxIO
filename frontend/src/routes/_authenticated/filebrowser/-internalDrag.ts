import { INTERNAL_DRAG_TYPE } from "./-dragTypes";

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
