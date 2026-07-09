import type { ReactNode } from "react";
import type {
  AppConfig,
  AppSettings,
  Dismissals,
  DockerProxySettings,
  DockerSettings,
  JobSettings,
} from "@/api";

export interface ConfigPatch {
  appSettings?: Partial<AppSettings>;
  dismissals?: Partial<Dismissals>;
  docker?: Partial<Omit<DockerSettings, "proxy">> & {
    proxy?: Partial<DockerProxySettings>;
  };
  jobs?: Partial<JobSettings>;
}

export type ConfigValueMap = AppSettings;
export type ConfigValueKey = keyof AppSettings;

export interface ConfigContextType {
  config: AppConfig;
  isLoaded: boolean;
  setKey: <K extends ConfigValueKey>(
    key: K,
    value: ConfigValueMap[K] | ((prev: ConfigValueMap[K]) => ConfigValueMap[K]),
  ) => void;
  /**
   * Apply a patch locally and persist it. `onSaved` fires only after the
   * backend confirms the save — domain-specific feedback (toasts,
   * follow-ups) belongs there, at the call site, not in the provider.
   */
  updateConfig: (
    patch: ConfigPatch | ((prev: AppConfig) => ConfigPatch),
    onSaved?: () => void,
  ) => void;
}

export interface ConfigProviderProps {
  children: ReactNode;
}
