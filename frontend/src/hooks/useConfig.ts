// src/hooks/useConfig.ts
import { useContext, useCallback } from "react";
import { ConfigContext } from "@/contexts/ConfigContext";
import { AppConfig } from "@/types/config";
import { setThemeColor, setDarkMode } from "@/utils/filebrowserCache";
import {
  setFBPrimaryToken,
  setFBDarkMode,
  isFBVisible,
  bgReloadFBIfHidden,
} from "@/utils/filebrowserDOM";

function resolveNext<T>(prev: T, next: T | ((p: T) => T)): T {
  return typeof next === "function" ? (next as (p: T) => T)(prev) : next;
}

const useConfig = () => {
  const ctx = useContext(ConfigContext);
  if (!ctx) throw new Error("useConfig must be used within ConfigProvider");
  return ctx;
};

export function useConfigValue<K extends keyof AppConfig>(key: K) {
  const { config, setKey } = useConfig();

  const set = useCallback(
    (v: AppConfig[K] | ((prev: AppConfig[K]) => AppConfig[K])) => {
      const current = config[key];
      const next = resolveNext(current, v);
      setKey(key, next);

      if (key === "primaryColor") {
        const token = String(next);
        void setThemeColor(token).catch(() => undefined);
        setFBPrimaryToken(token); // live update, best-effort
      }

      if (key === "theme") {
        const dark = String(next).toUpperCase() === "DARK";

        (async () => {
          if (isFBVisible()) {
            setFBDarkMode(dark); // live patch when visible
          } else {
            try {
              await setDarkMode(dark);
            } catch {
              // ignore; we'll still live-patch below
            }
            bgReloadFBIfHidden(); // reload hidden iframe AFTER prefs saved
          }
        })();
      }
    },
    [config, key, setKey]
  );

  return [config[key], set] as const;
}

export function useConfigReady(): boolean {
  const { isLoaded } = useConfig();
  return isLoaded;
}
