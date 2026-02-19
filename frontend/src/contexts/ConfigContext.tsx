// src/contexts/ConfigContext.tsx
import { useQueryClient } from "@tanstack/react-query";
import React, {
  createContext,
  useEffect,
  useState,
  useCallback,
  useMemo,
} from "react";
import { toast } from "sonner";

import { linuxio, CACHE_TTL_MS, LinuxIOError, waitForStreamMux } from "@/api";
import useAuth from "@/hooks/useAuth";
import {
  AppConfig,
  BackendSettings,
  ConfigContextType,
  ConfigProviderProps,
} from "@/types/config";

// Transform backend settings to frontend flat config
const fromBackendSettings = (settings: BackendSettings): AppConfig => ({
  theme: settings.appSettings.theme,
  primaryColor: settings.appSettings.primaryColor,
  sidebarCollapsed: settings.appSettings.sidebarCollapsed,
  showHiddenFiles: settings.appSettings.showHiddenFiles,
  dashboardOrder: settings.appSettings.dashboardOrder,
  hiddenCards: settings.appSettings.hiddenCards,
  containerOrder: settings.appSettings.containerOrder,
  dockerDashboardSections: settings.appSettings.dockerDashboardSections,
  dockerFolder: settings.docker.folder,
});

// Transform frontend config to backend settings format (partial update)
const toBackendSettings = (config: Partial<AppConfig>) => {
  const payload: any = {};

  // Map flat config to nested structure
  if (
    config.theme !== undefined ||
    config.primaryColor !== undefined ||
    config.sidebarCollapsed !== undefined ||
    config.showHiddenFiles !== undefined ||
    config.dashboardOrder !== undefined ||
    config.hiddenCards !== undefined ||
    config.containerOrder !== undefined ||
    config.dockerDashboardSections !== undefined
  ) {
    payload.appSettings = {};
    if (config.theme !== undefined) payload.appSettings.theme = config.theme;
    if (config.primaryColor !== undefined)
      payload.appSettings.primaryColor = config.primaryColor;
    if (config.sidebarCollapsed !== undefined)
      payload.appSettings.sidebarCollapsed = config.sidebarCollapsed;
    if (config.showHiddenFiles !== undefined)
      payload.appSettings.showHiddenFiles = config.showHiddenFiles;
    if (config.dashboardOrder !== undefined)
      payload.appSettings.dashboardOrder = config.dashboardOrder;
    if (config.hiddenCards !== undefined)
      payload.appSettings.hiddenCards = config.hiddenCards;
    if (config.containerOrder !== undefined)
      payload.appSettings.containerOrder = config.containerOrder;
    if (config.dockerDashboardSections !== undefined)
      payload.appSettings.dockerDashboardSections =
        config.dockerDashboardSections;
  }

  if (config.dockerFolder !== undefined) {
    payload.docker = { folder: config.dockerFolder };
  }

  return payload;
};

const defaultConfig: AppConfig = {
  theme: "DARK",
  primaryColor: "#2196f3",
  sidebarCollapsed: false,
  showHiddenFiles: false,
  dockerFolder: undefined,
  dashboardOrder: undefined,
  hiddenCards: undefined,
  containerOrder: undefined,
  dockerDashboardSections: undefined,
};

const applyDefaults = (
  cfg: Partial<AppConfig> | undefined | null,
): AppConfig => ({
  theme: cfg?.theme ?? defaultConfig.theme,
  primaryColor: cfg?.primaryColor ?? defaultConfig.primaryColor,
  sidebarCollapsed: cfg?.sidebarCollapsed ?? defaultConfig.sidebarCollapsed,
  showHiddenFiles: cfg?.showHiddenFiles ?? defaultConfig.showHiddenFiles,
  dashboardOrder: cfg?.dashboardOrder ?? defaultConfig.dashboardOrder,
  hiddenCards: cfg?.hiddenCards ?? defaultConfig.hiddenCards,
  containerOrder: cfg?.containerOrder ?? defaultConfig.containerOrder,
  dockerDashboardSections:
    cfg?.dockerDashboardSections ?? defaultConfig.dockerDashboardSections,
  dockerFolder: cfg?.dockerFolder ?? defaultConfig.dockerFolder,
});

export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);

export const ConfigProvider: React.FC<ConfigProviderProps> = ({ children }) => {
  const [config, setConfig] = useState<AppConfig>(defaultConfig);
  const [isLoaded, setLoaded] = useState(false);
  // Track if we successfully loaded from backend - only allow saves if true
  const [canSave, setCanSave] = useState(false);
  const { signOut } = useAuth();
  const queryClient = useQueryClient();
  const { mutate: setConfigRemote } = linuxio.config.set.useMutation();

  useEffect(() => {
    let cancelled = false;
    let retryTimeout: ReturnType<typeof setTimeout> | null = null;

    const fetchConfig = async (attempt = 1): Promise<void> => {
      if (cancelled) return;

      try {
        // Wait for stream mux to be ready (250ms per attempt, up to 5 attempts = 2.5s total)
        const muxReady = await waitForStreamMux(250);
        if (cancelled) return;

        if (!muxReady) {
          // Mux not ready - retry quickly (100ms delay between attempts)
          if (attempt < 5) {
            retryTimeout = setTimeout(() => fetchConfig(attempt + 1), 100);
            return;
          }
          // After 5 attempts, use defaults but don't allow saving
          console.warn("Stream mux not ready, using default config");
          setLoaded(true);
          // canSave stays false - prevent overwriting backend config with defaults
          return;
        }

        const settings = await queryClient.fetchQuery<BackendSettings>(
          linuxio.config.get.queryOptions({ staleTime: CACHE_TTL_MS.NONE }),
        );

        if (!cancelled) {
          setConfig(applyDefaults(fromBackendSettings(settings)));
          setCanSave(true); // Successfully loaded from backend, allow saves
          setLoaded(true);
        }
      } catch (error: unknown) {
        if (cancelled) return;

        // Don't treat stream errors as auth errors - just use defaults
        if (error instanceof LinuxIOError && error.code === 503) {
          console.warn("Stream API unavailable, using default config");
          setLoaded(true);
          // canSave stays false
          return;
        }

        // Only treat actual auth errors (401/403) as session expired
        const code = error instanceof LinuxIOError ? error.code : 500;
        if (code === 401 || code === 403) {
          toast.error("Session expired. Please sign in again.");
          await signOut();
          return;
        }

        // For other errors, just log and use defaults
        console.error("Failed to load config:", error);
        setLoaded(true);
        // canSave stays false
      }
    };

    fetchConfig();

    return () => {
      cancelled = true;
      if (retryTimeout) clearTimeout(retryTimeout);
    };
  }, [queryClient, signOut]);

  const save = useCallback(
    (cfg: AppConfig) => {
      if (!canSave) return; // Only save if we successfully loaded from backend
      const payload = toBackendSettings(cfg);
      setConfigRemote([JSON.stringify(payload)]);
    },
    [canSave, setConfigRemote],
  );

  const setKey: ConfigContextType["setKey"] = useCallback(
    (key, value) => {
      setConfig((prev) => {
        const nextVal = typeof value === "function" ? value(prev[key]) : value;
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
