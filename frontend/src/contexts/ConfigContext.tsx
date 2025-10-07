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

const initialConfig = {} as AppConfig;
export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);

export const ConfigProvider: React.FC<ConfigProviderProps> = ({ children }) => {
  const [config, setConfig] = useState<AppConfig>(initialConfig);
  const [isLoaded, setLoaded] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    (async () => {
      try {
        const r = await axios.get("/theme/get", { signal: controller.signal });
        setConfig(r.data);
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
        const next = { ...prev, [key]: nextVal } as AppConfig;
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
        const next = { ...prev, ...partial };
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
