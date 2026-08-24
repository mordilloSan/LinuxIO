// src/hooks/useConfig.ts
import { useQuery } from "@tanstack/react-query";
import { useCallback, useContext } from "react";

import {
  type AppConfig,
  type DockerSettings,
  type TableCardViewMode,
  type UIConfig,
  linuxio,
} from "@/api";
import {
  bridgeConfigQueryKey,
  isBridgeAppSettingKey,
  pruneViewModes,
  uiConfigQueryKey,
} from "@/api/config-query";
import { AuthContext } from "@/contexts/AuthContext";
import { ConfigContext } from "@/contexts/ConfigContext";
import type { ConfigValueKey, ConfigValueMap } from "@/types/config";

export const useConfig = () => {
  const ctx = useContext(ConfigContext);
  if (!ctx) throw new Error("useConfig must be used within ConfigProvider");
  return ctx;
};

/** Same per-user cache scope the provider writes under. */
export function useConfigUserId(): string {
  const auth = useContext(AuthContext);
  return auth?.user?.id ?? "anonymous";
}

// The read hooks subscribe to the query cache without ever fetching
// (enabled: false): ConfigProvider owns the load/reload lifecycle and gates
// children until both snapshots are cached, so subscribers always see data.

function useBridgeConfigValue(key: ConfigValueKey, userId: string): unknown {
  const { data } = useQuery({
    ...linuxio.config.get,
    queryKey: bridgeConfigQueryKey(userId),
    enabled: false,
    select: (config: AppConfig) =>
      isBridgeAppSettingKey(key) ? config.appSettings[key] : undefined,
  });
  return data;
}

function useUIConfigValue(key: ConfigValueKey, userId: string): unknown {
  const { data } = useQuery({
    ...linuxio.config.get_ui,
    queryKey: uiConfigQueryKey(userId),
    enabled: false,
    select: (config: UIConfig) => {
      if (isBridgeAppSettingKey(key)) return undefined;
      if (key === "viewModes") {
        return pruneViewModes(config.viewModes, config.viewModeDefault);
      }
      return config[key as keyof UIConfig];
    },
  });
  return data;
}

export function useConfigValue<K extends ConfigValueKey>(key: K) {
  const { setKey } = useConfig();
  const userId = useConfigUserId();
  const bridgeValue = useBridgeConfigValue(key, userId);
  const uiValue = useUIConfigValue(key, userId);
  const value = (
    isBridgeAppSettingKey(key) ? bridgeValue : uiValue
  ) as ConfigValueMap[K];

  const set = useCallback(
    (
      next:
        | ConfigValueMap[K]
        | ((prev: ConfigValueMap[K]) => ConfigValueMap[K]),
    ) => {
      setKey(key, next);
    },
    [key, setKey],
  );

  return [value, set] as const;
}

/** Backend view-mode policy; read-only, so it lives outside ConfigValueMap. */
export function useViewModeDefault(): TableCardViewMode {
  const userId = useConfigUserId();
  const { data } = useQuery({
    ...linuxio.config.get_ui,
    queryKey: uiConfigQueryKey(userId),
    enabled: false,
    select: (config: UIConfig) => config.viewModeDefault,
  });
  return data ?? "card";
}

export function useDockerSettings(): DockerSettings {
  const userId = useConfigUserId();
  const { data } = useQuery({
    ...linuxio.config.get,
    queryKey: bridgeConfigQueryKey(userId),
    enabled: false,
    select: (config: AppConfig) => config.docker,
  });
  if (!data) {
    throw new Error(
      "useDockerSettings must be used under a loaded ConfigProvider",
    );
  }
  return data;
}
