import React, { createContext, useEffect, useState, useCallback, useMemo } from "react";
import { DEFAULT_PRIMARY_COLOR, SIDEBAR_COLAPSED_STATE, THEMES } from "@/constants";
import { AppConfig, ConfigContextType, ConfigProviderProps } from "@/types/config";
import axios from "@/utils/axios";
import { debounce } from "@/utils/debounce";

const initialConfig: AppConfig = {
  theme: THEMES.DARK,
  primaryColor: DEFAULT_PRIMARY_COLOR,
  sidebarCollapsed: SIDEBAR_COLAPSED_STATE,
};

export const ConfigContext = createContext<ConfigContextType | undefined>(undefined);

export const ConfigProvider: React.FC<ConfigProviderProps> = ({ children }) => {
  const [config, setConfig] = useState<AppConfig>(initialConfig);
  const [isLoaded, setIsLoaded] = useState(false);
  const [canPersist, setCanPersist] = useState(false);

  // --- Load from backend ---
  useEffect(() => {
    let mounted = true;
    axios.get("/theme/get")
      .then(r => {
        if (!mounted) return;
        setConfig(prev => ({
          ...prev,
          ...r.data,
        }));
        setCanPersist(true);
      })
      .catch(() => { })
      .finally(() => mounted && setIsLoaded(true));
    return () => { mounted = false; };
  }, []);

  // --- Persist with debounce ---
  const saveConfig = useCallback((cfg: AppConfig) => {
    if (!canPersist) return;
    axios.post("/theme/set", cfg);
  }, [canPersist]);

  const debouncedSave = useMemo(() => debounce(saveConfig, 400), [saveConfig]);

  // --- Generic setters ---
  const setKey: ConfigContextType["setKey"] = useCallback((key, value) => {
    setConfig(prev => {
      const nextVal = typeof value === "function"
        ? (value as (p: typeof prev[typeof key]) => typeof prev[typeof key])(prev[key])
        : value;

      const next = { ...prev, [key]: nextVal } as AppConfig;
      debouncedSave(next);
      return next;
    });
  }, [debouncedSave]);

  const updateConfig: ConfigContextType["updateConfig"] = useCallback((patch) => {
    setConfig(prev => {
      const partial = typeof patch === "function" ? patch(prev) : patch;
      const next = { ...prev, ...partial };
      debouncedSave(next);
      return next;
    });
  }, [debouncedSave]);

  const contextValue = useMemo(() => ({
    config,
    setKey,
    updateConfig,
    isLoaded,
  }), [config, setKey, updateConfig, isLoaded]);

  return (
    <ConfigContext.Provider value={contextValue}>
      {children}
    </ConfigContext.Provider>
  );
};
