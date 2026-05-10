// src/hooks/useConfig.ts
import { useContext, useCallback } from "react";
import { ConfigContext } from "@/contexts/ConfigContext";
import {
  AppSettings,
  AppConfig,
  ConfigValueKey,
  ConfigValueMap,
} from "@/types/config";

export const useConfig = () => {
  const ctx = useContext(ConfigContext);
  if (!ctx) throw new Error("useConfig must be used within ConfigProvider");
  return ctx;
};

const readConfigValue = <K extends ConfigValueKey>(
  config: AppConfig,
  key: K,
): ConfigValueMap[K] => {
  return config.appSettings[key as keyof AppSettings] as ConfigValueMap[K];
};

export function useConfigValue<K extends ConfigValueKey>(key: K) {
  const { config, setKey } = useConfig();

  const set = useCallback(
    (
      next:
        | ConfigValueMap[K]
        | ((prev: ConfigValueMap[K]) => ConfigValueMap[K]),
    ) => {
      const cur = readConfigValue(config, key);
      const val = typeof next === "function" ? (next as any)(cur) : next;
      if (Object.is(cur, val)) return;
      setKey(key, val);
    },
    [config, key, setKey],
  );

  return [readConfigValue(config, key), set] as const;
}

export function useConfigReady(): boolean {
  const { isLoaded } = useConfig();
  return isLoaded;
}
