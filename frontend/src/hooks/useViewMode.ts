import { useCallback } from "react";

import { useConfigValue } from "@/hooks/useConfig";
import { TableCardViewMode } from "@/types/config";

export function useViewMode(
  key: string,
  fallback: TableCardViewMode = "table",
) {
  const [viewModes, setViewModes] = useConfigValue("viewModes");
  const viewMode = viewModes?.[key] ?? fallback;

  const setViewMode = useCallback(
    (
      next:
        | TableCardViewMode
        | ((prev: TableCardViewMode) => TableCardViewMode),
    ) => {
      setViewModes((prev) => {
        const prevModes = prev ?? {};
        const current = prevModes[key] ?? fallback;
        const resolved = typeof next === "function" ? next(current) : next;

        if (resolved === fallback) {
          if (!(key in prevModes)) return prev;
          const rest = { ...prevModes };
          delete rest[key];
          return Object.keys(rest).length > 0 ? rest : undefined;
        }

        if (prevModes[key] === resolved) return prev;
        return { ...prevModes, [key]: resolved };
      });
    },
    [fallback, key, setViewModes],
  );

  return [viewMode, setViewMode] as const;
}
