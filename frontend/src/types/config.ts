import type { ReactNode } from "react";

import type {
  AppConfig,
  AppSettings,
  ConfigDockAccentGradient,
  ConfigDockerDashboardSections,
  ConfigHardwareSections,
  ConfigThemeColorsByModePayload,
  Dismissals,
  DockTileColors,
  DockerProxySettings,
  DockerSettings,
  JobSettings,
  NavigationMode,
  TableCardViewMode,
  Theme,
} from "@/api";

/** Presentation preferences are owned by the frontend/UI state file. */
export interface EffectiveUISettings {
  theme: Theme;
  primaryColor: string;
  themeColors?: ConfigThemeColorsByModePayload;
  sidebarCollapsed: boolean;
  navigationMode: NavigationMode;
  dockTileColors: DockTileColors;
  dockAccentGradient: ConfigDockAccentGradient;
  hiddenCards: string[];
  dockerDashboardSections: ConfigDockerDashboardSections;
  hardwareSections: ConfigHardwareSections;
  viewModes: Record<string, TableCardViewMode>;
  viewModeDefault: TableCardViewMode;
  layoutOrders: Record<string, string[]>;
  terminalFontSize: number;
}

export type EffectiveAppSettings = AppSettings & EffectiveUISettings;
export type EffectiveAppConfig = Omit<AppConfig, "appSettings"> & {
  appSettings: EffectiveAppSettings;
};

// viewModeDefault is returned by the backend as policy, not a user-editable
// preference, so it is deliberately absent from the config mutation surface.
export type ConfigValueMap = Omit<EffectiveAppSettings, "viewModeDefault">;
export type ConfigValueKey = keyof ConfigValueMap;

export interface ConfigPatch {
  appSettings?: Partial<ConfigValueMap>;
  dismissals?: Partial<Dismissals>;
  docker?: Partial<Omit<DockerSettings, "proxy">> & {
    proxy?: Partial<DockerProxySettings>;
  };
  jobs?: Partial<JobSettings>;
}

export interface ConfigContextType {
  config: EffectiveAppConfig;
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
    patch: ConfigPatch | ((prev: EffectiveAppConfig) => ConfigPatch),
    onSaved?: () => void,
  ) => void;
}

export interface ConfigProviderProps {
  children: ReactNode;
}
