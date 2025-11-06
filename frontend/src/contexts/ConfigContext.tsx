// src/contexts/ConfigContext.tsx
import React, {
  createContext,
  useEffect,
  useState,
  useCallback,
  useMemo,
} from "react";
import { toast } from "sonner";

import {
  AppConfig,
  ConfigContextType,
  ConfigProviderProps,
} from "@/types/config";
import axios from "@/utils/axios";

const defaultConfig: AppConfig = {
  theme: "DARK",
  primaryColor: "#2196f3",
  sidebarCollapsed: false,
  showHiddenFiles: false,
};

const applyDefaults = (
  cfg: Partial<AppConfig> | undefined | null,
): AppConfig => ({
  theme: cfg?.theme ?? defaultConfig.theme,
  primaryColor: cfg?.primaryColor ?? defaultConfig.primaryColor,
  sidebarCollapsed: cfg?.sidebarCollapsed ?? defaultConfig.sidebarCollapsed,
  showHiddenFiles: cfg?.showHiddenFiles ?? defaultConfig.showHiddenFiles,
});

export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);

export const ConfigProvider: React.FC<ConfigProviderProps> = ({ children }) => {
  const [config, setConfig] = useState<AppConfig>(defaultConfig);
  const [isLoaded, setLoaded] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    (async () => {
      try {
        const r = await axios.get("/theme/get", { signal: controller.signal });
        setConfig(applyDefaults(r.data));
      } catch {
        toast.error("Session expired. Please sign in again.");
        window.location.assign("/sign-in");
        return;
      } finally {
        setLoaded(true);
      }
    })();
    return () => controller.abort();
  }, []);

  const save = useCallback(
    (cfg: AppConfig) => {
      if (!isLoaded) return;
      axios.post("/theme/set", cfg).catch(() => {});
    },
    [isLoaded],
  );

  const setKey: ConfigContextType["setKey"] = useCallback(
    (key, value) => {
      setConfig((prev) => {
        const nextVal =
          typeof value === "function" ? (value as any)(prev[key]) : value;
        if (Object.is(prev[key], nextVal)) return prev;
        const next = applyDefaults({ ...prev, [key]: nextVal });
        save(next);
        return next;
      });
    },
    [save],
  );

  const updateConfig: ConfigContextType["updateConfig"] = useCallback(
    (patch) => {
      setConfig((prev) => {
        const partial = typeof patch === "function" ? patch(prev) : patch;
        const next = applyDefaults({ ...prev, ...partial });
        save(next);
        return next;
      });
    },
    [save],
  );

  const value = useMemo(
    () => ({ config, setKey, updateConfig, isLoaded }),
    [config, setKey, updateConfig, isLoaded],
  );
  if (!isLoaded) return null;
  return (
    <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
  );
};
