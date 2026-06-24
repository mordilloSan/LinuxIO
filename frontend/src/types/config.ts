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
  updateConfig: (
    patch: ConfigPatch | ((prev: AppConfig) => ConfigPatch),
  ) => void;
}

export interface ConfigProviderProps {
  children: React.ReactNode;
}
