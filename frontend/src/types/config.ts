export type TableCardViewMode = "card" | "table";
export type AppViewModes = Record<string, TableCardViewMode>;

export interface ThemeColors {
  backgroundDefault?: string;
  backgroundPaper?: string;
  headerBackground?: string;
  footerBackground?: string;
  sidebarBackground?: string;
  cardBackground?: string;
  dialogBorder?: string;
  dialogGlow?: string;
  dialogBackdrop?: string;
  codeBackground?: string;
  codeText?: string;
  chartRx?: string;
  chartTx?: string;
  chartNeutral?: string;
  fileBrowserSurface?: string;
  fileBrowserChrome?: string;
  fileBrowserBreadcrumbBackground?: string;
  fileBrowserBreadcrumbText?: string;
}

// Backend Settings structure
export interface BackendSettings {
  appSettings: {
    theme: string;
    primaryColor: string;
    themeColors?: ThemeColors;
    sidebarCollapsed: boolean;
    showHiddenFiles: boolean;
    dashboardOrder?: string[];
    hiddenCards?: string[];
    containerOrder?: string[];
    dockerDashboardSections?: {
      overview: boolean;
      daemon: boolean;
      resources: boolean;
    };
    hardwareSections?: {
      overview: boolean;
      hardware: boolean;
      sensors: boolean;
      systemInfo: boolean;
      gpu: boolean;
      pciDevices: boolean;
      memoryModules: boolean;
    };
    viewModes?: AppViewModes;
    chunkSizeMB?: number;
  };
  docker: {
    folder: string;
  };
  jobs?: JobSettings;
}

export interface JobSettings {
  progressMinIntervalMs: number;
  notificationMinIntervalMs: number;
  progressMinBytesMB: number;
  heavyArchiveConcurrency: number;
  archiveCompressionWorkers: number;
  archiveExtractWorkers: number;
}

// Flattened config for frontend use
export interface AppConfig {
  theme: string;
  primaryColor: string;
  themeColors?: ThemeColors;
  sidebarCollapsed: boolean;
  showHiddenFiles: boolean;
  dockerFolder?: string;
  dashboardOrder?: string[];
  hiddenCards?: string[];
  containerOrder?: string[];
  dockerDashboardSections?: {
    overview: boolean;
    daemon: boolean;
    resources: boolean;
  };
  hardwareSections?: {
    overview: boolean;
    hardware: boolean;
    sensors: boolean;
    systemInfo: boolean;
    gpu: boolean;
    pciDevices: boolean;
    memoryModules: boolean;
  };
  viewModes?: AppViewModes;
  chunkSizeMB?: number;
  jobs?: JobSettings;
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
