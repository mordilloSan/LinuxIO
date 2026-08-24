import type { QueryKey } from "@tanstack/react-query";

import linuxio from "./generated/client";
import type { TableCardViewMode } from "./generated/linuxio-types";

/** Keep per-user configuration query entries isolated in the shared client. */
export function scopedConfigQueryKey(
  queryKey: QueryKey,
  userId: string,
): QueryKey {
  return [...queryKey, "user", userId];
}

/** Cache entry holding the bridge-owned config (`config.get`). */
export function bridgeConfigQueryKey(userId: string): QueryKey {
  return scopedConfigQueryKey(linuxio.config.get.queryKey, userId);
}

/** Cache entry holding the UI preference file (`config.get_ui`). */
export function uiConfigQueryKey(userId: string): QueryKey {
  return scopedConfigQueryKey(linuxio.config.get_ui.queryKey, userId);
}

/** App-setting keys persisted by the bridge config file, not the UI file. */
export const BRIDGE_APP_SETTING_KEYS = [
  "showHiddenFiles",
  "chunkSizeMB",
] as const;

export type BridgeAppSettingKey = (typeof BRIDGE_APP_SETTING_KEYS)[number];

export function isBridgeAppSettingKey(key: string): key is BridgeAppSettingKey {
  return (BRIDGE_APP_SETTING_KEYS as readonly string[]).includes(key);
}

let lastPruneViewModes: Record<string, TableCardViewMode> | null = null;
let lastPruneDefault: TableCardViewMode | null = null;
let lastPruneResult: Record<string, TableCardViewMode> = {};

/**
 * Stored view modes that match the backend default are inherited, not chosen,
 * so they are dropped from the effective map. Memoized on input identity:
 * query-cache structural sharing keeps `viewModes` stable across unrelated
 * writes, so every subscriber keeps receiving the same pruned object.
 */
export function pruneViewModes(
  viewModes: Record<string, TableCardViewMode>,
  viewModeDefault: TableCardViewMode,
): Record<string, TableCardViewMode> {
  if (
    lastPruneViewModes === viewModes &&
    lastPruneDefault === viewModeDefault
  ) {
    return lastPruneResult;
  }
  const pruned = Object.fromEntries(
    Object.entries(viewModes).filter(([, mode]) => mode !== viewModeDefault),
  );
  lastPruneViewModes = viewModes;
  lastPruneDefault = viewModeDefault;
  lastPruneResult = pruned;
  return pruned;
}
