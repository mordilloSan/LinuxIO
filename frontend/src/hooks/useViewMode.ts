import { useCallback } from "react";

import type { TableCardViewMode } from "@/api";
import { useConfigValue, useViewModeDefault } from "@/hooks/useConfig";

export function useViewMode(key: string) {
  const [viewModes, setViewModes] = useConfigValue("viewModes");
  const viewModeDefault = useViewModeDefault();
  const viewMode = viewModes[key] ?? viewModeDefault;

  const setViewMode = useCallback(
    (
      next:
        | TableCardViewMode
        | ((prev: TableCardViewMode) => TableCardViewMode),
    ) => {
      setViewModes((prev) => {
        const current = prev[key] ?? viewModeDefault;
        const resolved = typeof next === "function" ? next(current) : next;

        if (resolved === viewModeDefault) {
          if (!(key in prev)) return prev;
          const nextModes = { ...prev };
          delete nextModes[key];
          return nextModes;
        }
        if (prev[key] === resolved) return prev;
        return { ...prev, [key]: resolved };
      });
    },
    [key, setViewModes, viewModeDefault],
  );

  return [viewMode, setViewMode] as const;
}
