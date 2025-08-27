// src/hooks/useConfig.ts
import { useContext, useCallback } from "react";
import { ConfigContext } from "@/contexts/ConfigContext";
import { AppConfig } from "@/types/config";
import {
  setFilebrowserThemeColor,
  setFilebrowserDarkMode,
  liveSetPrimaryToken,
  liveSetDarkMode,
} from "@/utils/filebrowser";

const useConfig = () => {
  const ctx = useContext(ConfigContext);
  if (!ctx) throw new Error("useConfig must be used within ConfigProvider");
  return ctx;
};

export function useConfigValue<K extends keyof AppConfig>(key: K) {
  const { config, setKey } = useConfig();

  const effects = useCallback((k: keyof AppConfig, v: AppConfig[typeof k]) => {
    switch (k) {
      case "primaryColor": {
        const token = String(v ?? "").trim();
        void setFilebrowserThemeColor(token || undefined).catch(
          () => undefined,
        );
        liveSetPrimaryToken(token || undefined);
        return;
      }
      case "theme": {
        const dark = String(v).toUpperCase() === "DARK";
        void setFilebrowserDarkMode(dark).catch(() => undefined);
        liveSetDarkMode(dark);
        return;
      }
      default:
        return;
    }
  }, []);

  const set = useCallback(
    (next: AppConfig[K] | ((prev: AppConfig[K]) => AppConfig[K])) => {
      const cur = config[key];
      const val = typeof next === "function" ? (next as any)(cur) : next;
      if (Object.is(cur, val)) return;
      setKey(key, val);
      effects(key, val as AppConfig[K]);
    },
    [config, key, setKey, effects],
  );

  return [config[key], set] as const;
}

export function useConfigReady(): boolean {
  const { isLoaded } = useConfig();
  return isLoaded;
}
