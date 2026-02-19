// Backend Settings structure
export interface BackendSettings {
  appSettings: {
    theme: string;
    primaryColor: string;
    sidebarCollapsed: boolean;
    showHiddenFiles: boolean;
    dashboardOrder?: string[];
    hiddenCards?: string[];
    containerOrder?: string[];
    dockerDashboardSections?: { overview: boolean; daemon: boolean; resources: boolean };
  };
  docker: {
    folder: string;
  };
}

// Flattened config for frontend use
export interface AppConfig {
  theme: string;
  primaryColor: string;
  sidebarCollapsed: boolean;
  showHiddenFiles: boolean;
  dockerFolder?: string;
  dashboardOrder?: string[];
  hiddenCards?: string[];
  containerOrder?: string[];
  dockerDashboardSections?: { overview: boolean; daemon: boolean; resources: boolean };
}

export interface ConfigContextType {
  config: AppConfig;
  updateConfig: (
    patch: Partial<AppConfig> | ((prev: AppConfig) => Partial<AppConfig>),
  ) => void;
  setKey: <K extends keyof AppConfig>(
    key: K,
    value: AppConfig[K] | ((prev: AppConfig[K]) => AppConfig[K]),
  ) => void;
  isLoaded: boolean;
}

export interface ConfigProviderProps {
  children: React.ReactNode;
}
