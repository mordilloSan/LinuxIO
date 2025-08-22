export type AppConfig = {
  theme: string;
  primaryColor: string;
  sidebarCollapsed: boolean;
};

export type ConfigContextType = {
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

export type ConfigProviderProps = {
  children: React.ReactNode;
};
