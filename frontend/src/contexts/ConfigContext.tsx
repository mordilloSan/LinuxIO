// src/contexts/ConfigContext.tsx
import React, {
  createContext,
  useEffect,
  useState,
  useCallback,
  useMemo,
} from "react";
import { toast } from "sonner";

import useAuth from "@/hooks/useAuth";
import {
  AppConfig,
  ConfigContextType,
  ConfigProviderProps,
} from "@/types/config";
import { streamApi, StreamApiError } from "@/utils/streamApi";
import { waitForStreamMux } from "@/utils/StreamMultiplexer";

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
  const { signOut } = useAuth();

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        // Wait for stream mux to be ready before fetching config
        const muxReady = await waitForStreamMux(5000);
        if (cancelled) return;

        if (!muxReady) {
          // Mux not ready - use defaults, don't treat as auth error
          console.warn("Stream mux not ready, using default config");
          setLoaded(true);
          return;
        }

        const data = await streamApi.get<AppConfig>("config", "theme_get");
        if (!cancelled) {
          setConfig(applyDefaults(data));
        }
      } catch (error: unknown) {
        if (cancelled) return;

        // Don't treat stream errors as auth errors - just use defaults
        if (error instanceof StreamApiError && error.code === 503) {
          console.warn("Stream API unavailable, using default config");
          setLoaded(true);
          return;
        }

        // Only treat actual auth errors (401/403) as session expired
        const code = error instanceof StreamApiError ? error.code : 500;
        if (code === 401 || code === 403) {
          toast.error("Session expired. Please sign in again.");
          await signOut();
          return;
        }

        // For other errors, just log and use defaults
        console.error("Failed to load config:", error);
      } finally {
        if (!cancelled) {
          setLoaded(true);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [signOut]);

  const save = useCallback(
    (cfg: AppConfig) => {
      if (!isLoaded) return;
      streamApi.post("config", "theme_set", cfg).catch(() => {});
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
