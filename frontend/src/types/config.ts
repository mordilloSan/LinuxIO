export type TableCardViewMode = "card" | "table";
export type AppViewModes = Record<string, TableCardViewMode>;
export type Theme = "LIGHT" | "DARK";

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

export interface ThemeColorsByMode {
  light?: ThemeColors;
  dark?: ThemeColors;
}

export interface DockerDashboardSections {
  overview: boolean;
  daemon: boolean;
  resources: boolean;
}

export interface HardwareSections {
  overview: boolean;
  hardware: boolean;
  sensors: boolean;
  systemInfo: boolean;
  gpu: boolean;
  pciDevices: boolean;
  memoryModules: boolean;
}

export interface AppSettings {
  theme: Theme;
  primaryColor: string;
  themeColors?: ThemeColorsByMode;
  sidebarCollapsed: boolean;
  showHiddenFiles: boolean;
  dashboardOrder?: string[];
  hiddenCards?: string[];
  containerOrder?: string[];
  dockerDashboardSections?: DockerDashboardSections;
  hardwareSections?: HardwareSections;
  viewModes?: AppViewModes;
  chunkSizeMB?: number;
}

export interface DockerProxySettings {
  caddyEnabled: boolean;
  baseDomain?: string;
  tlsEmail?: string;
}

export interface DockerSettings {
  folders: string[];
  autoUpdateStacks?: string[];
  proxy: DockerProxySettings;
}

export interface JobSettings {
  progressMinIntervalMs: number;
  notificationMinIntervalMs: number;
  progressMinBytesMB: number;
  heavyArchiveConcurrency: number;
  archiveCompressionWorkers: number;
  archiveExtractWorkers: number;
}

export interface Dismissals {
  uncleanShutdownBootId?: string;
  failedLoginAlertId?: string;
}

export interface AppConfig {
  appSettings: AppSettings;
  docker: DockerSettings;
  jobs: JobSettings;
  dismissals?: Dismissals;
}

export interface ConfigPatch {
  appSettings?: Partial<AppSettings>;
  docker?: Partial<Omit<DockerSettings, "proxy">> & {
    proxy?: Partial<DockerProxySettings>;
  };
  jobs?: Partial<JobSettings>;
  dismissals?: Partial<Dismissals>;
}

export type ConfigValueMap = AppSettings;
export type ConfigValueKey = keyof AppSettings;

export interface ConfigContextType {
  config: AppConfig;
  updateConfig: (
    patch: ConfigPatch | ((prev: AppConfig) => ConfigPatch),
  ) => void;
  setKey: <K extends ConfigValueKey>(
    key: K,
    value: ConfigValueMap[K] | ((prev: ConfigValueMap[K]) => ConfigValueMap[K]),
  ) => void;
  isLoaded: boolean;
}

export interface ConfigProviderProps {
  children: React.ReactNode;
}
