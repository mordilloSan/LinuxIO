// src/contexts/ConfigContext.tsx
import React, { createContext, useCallback, useMemo, useEffect } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
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

const CONFIG_KEY = ["config"];

const fetchConfig = async (): Promise<AppConfig> => {
  const r = await axios.get("/theme/get");
  return r.data as AppConfig;
};

const saveConfig = async (cfg: AppConfig): Promise<void> => {
  await axios.post("/theme/set", cfg);
};

const isAuthError = (err: any) => {
  const status = err?.response?.status;
  return status === 401 || status === 419 || status === 440;
};

export const ConfigProvider: React.FC<ConfigProviderProps> = ({ children }) => {
  const queryClient = useQueryClient();

  // Load config
  const {
    data: config = initialConfig,
    isSuccess,
    isLoading,
    isError,
    error,
  } = useQuery({
    queryKey: CONFIG_KEY,
    queryFn: fetchConfig,
    staleTime: 60_000,
    retry: 1,
  });

  // Auth redirect on initial load failure
  useEffect(() => {
    if (isError && isAuthError(error)) {
      toast.error("Session expired. Please sign in again.");
      window.location.assign("/sign-in");
    }
  }, [isError, error]);

  // Optimistic save
  const saveMutation = useMutation({
    mutationFn: saveConfig,
    onMutate: async (next: AppConfig) => {
      await queryClient.cancelQueries({ queryKey: CONFIG_KEY });
      const previous = queryClient.getQueryData<AppConfig>(CONFIG_KEY);
      queryClient.setQueryData<AppConfig>(CONFIG_KEY, next);
      return { previous };
    },
    onError: (err, _next, context) => {
      if (isAuthError(err)) {
        toast.error("Session expired. Please sign in again.");
        window.location.assign("/sign-in");
        return;
      }
      // rollback if we had previous
      if (context?.previous) {
        queryClient.setQueryData<AppConfig>(CONFIG_KEY, context.previous);
      }
      toast.error("Failed to save configuration.");
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: CONFIG_KEY });
    },
  });

  const save = useCallback(
    (cfg: AppConfig) => {
      if (!isSuccess) return; // mirror previous guard
      saveMutation.mutate(cfg);
    },
    [isSuccess, saveMutation],
  );

  const setKey: ConfigContextType["setKey"] = useCallback(
    (key, value) => {
      const current = queryClient.getQueryData<AppConfig>(CONFIG_KEY) ?? initialConfig;
      const nextVal =
        typeof value === "function" ? (value as any)(current[key]) : value;

      if (Object.is(current[key], nextVal)) return;

      const next = { ...current, [key]: nextVal } as AppConfig;
      save(next);
    },
    [queryClient, save],
  );

  const updateConfig: ConfigContextType["updateConfig"] = useCallback(
    (patch) => {
      const current = queryClient.getQueryData<AppConfig>(CONFIG_KEY) ?? initialConfig;
      const partial = typeof patch === "function" ? patch(current) : patch;
      const next = { ...current, ...partial } as AppConfig;
      save(next);
    },
    [queryClient, save],
  );

  const value = useMemo<ConfigContextType>(
    () => ({ config, setKey, updateConfig, isLoaded: isSuccess }),
    [config, setKey, updateConfig, isSuccess],
  );

  if (isLoading) return null;

  return (
    <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
  );
};
