export type TableCardViewMode = "card" | "table";
export type AppViewModes = Record<string, TableCardViewMode>;
export type Theme = "LIGHT" | "DARK";

export interface ThemeColors {
  backgroundDefault?: string;
  backgroundPaper?: string;
  cardBackground?: string;
  chartNeutral?: string;
  chartRx?: string;
  chartTx?: string;
  codeBackground?: string;
  codeText?: string;
  dialogBackdrop?: string;
  dialogBorder?: string;
  dialogGlow?: string;
  fileBrowserBreadcrumbBackground?: string;
  fileBrowserBreadcrumbText?: string;
  fileBrowserChrome?: string;
  fileBrowserSurface?: string;
  footerBackground?: string;
  headerBackground?: string;
  sidebarBackground?: string;
}

export interface ThemeColorsByMode {
  dark?: ThemeColors;
  light?: ThemeColors;
}

export interface DockerDashboardSections {
  daemon: boolean;
  overview: boolean;
  resources: boolean;
}

export interface HardwareSections {
  gpu: boolean;
  hardware: boolean;
  memoryModules: boolean;
  overview: boolean;
  pciDevices: boolean;
  sensors: boolean;
  systemInfo: boolean;
}

export interface AppSettings {
  chunkSizeMB?: number;
  containerOrder?: string[];
  dashboardOrder?: string[];
  dockerDashboardSections?: DockerDashboardSections;
  hardwareSections?: HardwareSections;
  hiddenCards?: string[];
  primaryColor: string;
  showHiddenFiles: boolean;
  sidebarCollapsed: boolean;
  theme: Theme;
  themeColors?: ThemeColorsByMode;
  viewModes?: AppViewModes;
}

export interface DockerProxySettings {
  baseDomain?: string;
  caddyEnabled: boolean;
  tlsEmail?: string;
}

export interface DockerSettings {
  autoUpdateStacks?: string[];
  folders: string[];
  proxy: DockerProxySettings;
}

export interface JobSettings {
  archiveCompressionWorkers: number;
  archiveExtractWorkers: number;
  heavyArchiveConcurrency: number;
  notificationMinIntervalMs: number;
  progressMinBytesMB: number;
  progressMinIntervalMs: number;
}

export interface Dismissals {
  failedLoginAlertId?: string;
  uncleanShutdownBootId?: string;
}

export interface AppConfig {
  appSettings: AppSettings;
  dismissals?: Dismissals;
  docker: DockerSettings;
  jobs: JobSettings;
}

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
