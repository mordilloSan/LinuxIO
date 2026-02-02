// Backend Settings structure
export interface BackendSettings {
  appSettings: {
    theme: string;
    primaryColor: string;
    sidebarCollapsed: boolean;
    showHiddenFiles: boolean;
  };
  docker: {
    folder: string;
  };
};

// Flattened config for frontend use
export interface AppConfig {
  theme: string;
  primaryColor: string;
  sidebarCollapsed: boolean;
  showHiddenFiles: boolean;
  dockerFolder?: string;
};

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
};

export interface ConfigProviderProps {
  children: React.ReactNode;
};
