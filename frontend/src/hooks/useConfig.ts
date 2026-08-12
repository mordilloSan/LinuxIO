// src/hooks/useConfig.ts
import { useCallback, useContext } from "react";

import type { AppConfig } from "@/api";
import { ConfigContext } from "@/contexts/ConfigContext";
import type { ConfigValueKey, ConfigValueMap } from "@/types/config";

export const useConfig = () => {
  const ctx = useContext(ConfigContext);
  if (!ctx) throw new Error("useConfig must be used within ConfigProvider");
  return ctx;
};

const readConfigValue = <K extends ConfigValueKey>(
  config: AppConfig,
  key: K,
): ConfigValueMap[K] => {
  return config.appSettings[key];
};

export function useConfigValue<K extends ConfigValueKey>(key: K) {
  const { config, setKey } = useConfig();

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

  return [readConfigValue(config, key), set] as const;
}
