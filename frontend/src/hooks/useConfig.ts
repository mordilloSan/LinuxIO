import { useContext, useCallback } from "react";
import { ConfigContext } from "@/contexts/ConfigContext";
import { AppConfig } from "@/types/config";

const useConfig = () => {
  const ctx = useContext(ConfigContext);
  if (!ctx) throw new Error("useConfig must be used within ConfigProvider");
  return ctx;
};

export default useConfig;

// useConfigValue — select one key with type-safe getter+setter
export function useConfigValue<K extends keyof AppConfig>(key: K) {
  const { config, setKey } = useConfig();
  const set = useCallback(
    (v: AppConfig[K] | ((prev: AppConfig[K]) => AppConfig[K])) =>
      setKey(key, v),
    [key, setKey]
  );
  return [config[key], set] as const;
}
