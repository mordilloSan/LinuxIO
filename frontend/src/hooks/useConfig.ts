// src/hooks/useConfig.ts
import { useContext, useCallback } from "react";
import { ConfigContext } from "@/contexts/ConfigContext";
import { AppConfig } from "@/types/config";

const useConfig = () => {
  const ctx = useContext(ConfigContext);
  if (!ctx) throw new Error("useConfig must be used within ConfigProvider");
  return ctx;
};

export function useConfigValue<K extends keyof AppConfig>(key: K) {
  const { config, setKey } = useConfig();

  const set = useCallback(
    (next: AppConfig[K] | ((prev: AppConfig[K]) => AppConfig[K])) => {
      const cur = config[key];
      const val = typeof next === "function" ? (next as any)(cur) : next;
      if (Object.is(cur, val)) return;
      setKey(key, val);
    },
    [config, key, setKey],
  );

  return [config[key], set] as const;
}

export function useConfigReady(): boolean {
  const { isLoaded } = useConfig();
  return isLoaded;
}
