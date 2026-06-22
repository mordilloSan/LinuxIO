import type {
  AppConfig as ApiAppConfig,
  AppSettings as ApiAppSettings,
  ConfigDockerDashboardSections,
  ConfigHardwareSections,
  ConfigThemeColorsByModePayload,
  ConfigThemeColorsPayload,
  Dismissals as ApiDismissals,
  DockerProxySettings as ApiDockerProxySettings,
  DockerSettings as ApiDockerSettings,
  JobSettings as ApiJobSettings,
  TableCardViewMode,
  Theme,
} from "@/api";

export type { TableCardViewMode, Theme };

export type AppViewModes = Record<string, TableCardViewMode>;
export type ThemeColors = ConfigThemeColorsPayload;
export type ThemeColorsByMode = ConfigThemeColorsByModePayload;
export type DockerDashboardSections = ConfigDockerDashboardSections;
export type HardwareSections = ConfigHardwareSections;
export type AppSettings = ApiAppSettings;
export type DockerProxySettings = ApiDockerProxySettings;
export type DockerSettings = ApiDockerSettings;
export type JobSettings = ApiJobSettings;
export type Dismissals = ApiDismissals;
export type AppConfig = ApiAppConfig;

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
