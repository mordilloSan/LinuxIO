import { useCallback } from "react";

import type { TableCardViewMode } from "@/api";
import { useConfigValue } from "@/hooks/useConfig";

/* The app-wide default for every table/card toggle. A surface's mode is only
   stored when it differs from this, so changing it re-defaults every surface
   the user hasn't explicitly switched away from it. */
const DEFAULT_VIEW_MODE: TableCardViewMode = "card";

export function useViewMode(key: string) {
  const [viewModes, setViewModes] = useConfigValue("viewModes");
  const viewMode = viewModes?.[key] ?? DEFAULT_VIEW_MODE;

  const setViewMode = useCallback(
    (
      next:
        | TableCardViewMode
        | ((prev: TableCardViewMode) => TableCardViewMode),
    ) => {
      setViewModes((prev) => {
        const prevModes = prev ?? {};
        const current = prevModes[key] ?? DEFAULT_VIEW_MODE;
        const resolved = typeof next === "function" ? next(current) : next;

        if (resolved === DEFAULT_VIEW_MODE) {
          if (!(key in prevModes)) return prev;
          const rest = { ...prevModes };
          delete rest[key];
          return Object.keys(rest).length > 0 ? rest : undefined;
        }

        if (prevModes[key] === resolved) return prev;
        return { ...prevModes, [key]: resolved };
      });
    },
    [key, setViewModes],
  );

  return [viewMode, setViewMode] as const;
}
