// src/contexts/ConfigContext.tsx
import { useQueryClient } from "@tanstack/react-query";
import React, {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { toast } from "sonner";

import { CACHE_TTL_MS, linuxio, LinuxIOError, waitForStreamMux } from "@/api";
import useAuth from "@/hooks/useAuth";
import {
  AppConfig,
  AppSettings,
  AppViewModes,
  ConfigContextType,
  ConfigPatch,
  ConfigProviderProps,
  ConfigValueKey,
  ConfigValueMap,
  DockerDashboardSections,
  HardwareSections,
  TableCardViewMode,
  ThemeColors,
  ThemeColorsByMode,
} from "@/types/config";
import { readConfigCache, writeConfigCache } from "@/utils/configCache";

const isTableCardViewMode = (mode: unknown): mode is TableCardViewMode =>
  mode === "card" || mode === "table";

const normalizeViewModes = (
  viewModes: AppSettings["viewModes"] | undefined,
): AppViewModes | undefined => {
  if (!viewModes) return undefined;

  const normalized: AppViewModes = {};
  for (const [key, value] of Object.entries(viewModes)) {
    if (!key || !isTableCardViewMode(value)) continue;
    normalized[key] = value;
  }

  return Object.keys(normalized).length > 0 ? normalized : undefined;
};

const defaultThemeColors: ThemeColorsByMode = {
  light: {
    backgroundDefault: "#F7F9FC",
    backgroundPaper: "#FFFFFF",
    headerBackground: "#F7F9FC",
    footerBackground: "#F7F9FC",
    sidebarBackground: "#F7F9FC",
    cardBackground: "#FFFFFF",
    dialogBorder: "#FFFFFF",
    dialogGlow: "#FFFFFF",
    dialogBackdrop: "#000000",
    codeBackground: "#F5F5F5",
    codeText: "#333333",
    chartRx: "#8884D8",
    chartTx: "#82CA9D",
    chartNeutral: "#808080",
    fileBrowserSurface: "#FFFFFF",
    fileBrowserChrome: "#253137",
    fileBrowserBreadcrumbBackground: "#D0D4D8",
    fileBrowserBreadcrumbText: "#5A5A5A",
  },
  dark: {
    backgroundDefault: "#1B2635",
    backgroundPaper: "#233044",
    headerBackground: "#1B2635",
    footerBackground: "#1B2635",
    sidebarBackground: "#1B2635",
    cardBackground: "#11192A",
    dialogBorder: "#FFFFFF",
    dialogGlow: "#FFFFFF",
    dialogBackdrop: "#000000",
    codeBackground: "#1E1E1E",
    codeText: "#D4D4D4",
    chartRx: "#8884D8",
    chartTx: "#82CA9D",
    chartNeutral: "#808080",
    fileBrowserSurface: "#20292F",
    fileBrowserChrome: "#253137",
    fileBrowserBreadcrumbBackground: "#283136",
    fileBrowserBreadcrumbText: "#FFFFFF",
  },
};

const defaultConfig: AppConfig = {
  appSettings: {
    theme: "DARK",
    primaryColor: "#2196f3",
    themeColors: defaultThemeColors,
    sidebarCollapsed: false,
    showHiddenFiles: true,
    dashboardOrder: [
      "overview",
      "system",
      "cpu",
      "memory",
      "docker",
      "nic",
      "fs",
      "mb",
      "gpu",
      "drive",
    ],
    hiddenCards: [],
    containerOrder: [],
    dockerDashboardSections: {
      overview: true,
      daemon: true,
      resources: true,
    },
    hardwareSections: {
      overview: true,
      hardware: true,
      sensors: true,
      systemInfo: true,
      gpu: true,
      pciDevices: true,
      memoryModules: true,
    },
    viewModes: {
      "accounts.groups": "card",
      "accounts.users": "card",
      "docker.containers": "card",
      "docker.images": "card",
      "docker.networks": "card",
      "docker.stacks": "card",
      "docker.volumes": "card",
      "services.list": "card",
      shares: "card",
      "shares.mounts": "card",
      "sockets.list": "card",
      "timers.list": "card",
    },
    chunkSizeMB: 1,
  },
  docker: {
    folders: ["/var/lib/linuxio/docker"],
    proxy: {
      caddyEnabled: false,
      baseDomain: "",
      tlsEmail: "",
    },
  },
  jobs: {
    progressMinIntervalMs: 250,
    notificationMinIntervalMs: 1000,
    progressMinBytesMB: 16,
    heavyArchiveConcurrency: 1,
    archiveCompressionWorkers: 0,
    archiveExtractWorkers: 0,
  },
};

const cloneThemeColors = (colors?: ThemeColors): ThemeColors | undefined =>
  colors ? { ...colors } : undefined;

const cloneThemeColorsByMode = (
  colors?: ThemeColorsByMode,
): ThemeColorsByMode | undefined =>
  colors
    ? {
        light: cloneThemeColors(colors.light),
        dark: cloneThemeColors(colors.dark),
      }
    : undefined;

const cloneArray = <T,>(items?: T[]): T[] | undefined =>
  items ? [...items] : undefined;

const cloneRecord = <T,>(
  value?: Record<string, T>,
): Record<string, T> | undefined => (value ? { ...value } : undefined);

const cloneDockerDashboardSections = (
  sections?: DockerDashboardSections,
): DockerDashboardSections | undefined =>
  sections ? { ...sections } : undefined;

const cloneHardwareSections = (
  sections?: HardwareSections,
): HardwareSections | undefined => (sections ? { ...sections } : undefined);

const applyDefaults = (
  cfg: ConfigPatch | Partial<AppConfig> | null,
): AppConfig => {
  const app: Partial<AppSettings> = cfg?.appSettings ?? {};
  const docker: NonNullable<ConfigPatch["docker"]> = cfg?.docker ?? {};
  const jobs: Partial<AppConfig["jobs"]> = cfg?.jobs ?? {};
  const viewModes =
    normalizeViewModes(app.viewModes) ??
    cloneRecord(defaultConfig.appSettings.viewModes);

  return {
    appSettings: {
      theme: app.theme ?? defaultConfig.appSettings.theme,
      primaryColor: app.primaryColor ?? defaultConfig.appSettings.primaryColor,
      themeColors: cloneThemeColorsByMode(
        app.themeColors ?? defaultConfig.appSettings.themeColors,
      ),
      sidebarCollapsed:
        app.sidebarCollapsed ?? defaultConfig.appSettings.sidebarCollapsed,
      showHiddenFiles:
        app.showHiddenFiles ?? defaultConfig.appSettings.showHiddenFiles,
      dashboardOrder:
        cloneArray(app.dashboardOrder) ??
        cloneArray(defaultConfig.appSettings.dashboardOrder),
      hiddenCards:
        cloneArray(app.hiddenCards) ??
        cloneArray(defaultConfig.appSettings.hiddenCards),
      containerOrder:
        cloneArray(app.containerOrder) ??
        cloneArray(defaultConfig.appSettings.containerOrder),
      dockerDashboardSections:
        cloneDockerDashboardSections(app.dockerDashboardSections) ??
        cloneDockerDashboardSections(
          defaultConfig.appSettings.dockerDashboardSections,
        ),
      hardwareSections:
        cloneHardwareSections(app.hardwareSections) ??
        cloneHardwareSections(defaultConfig.appSettings.hardwareSections),
      viewModes,
      chunkSizeMB: app.chunkSizeMB ?? defaultConfig.appSettings.chunkSizeMB,
    },
    docker: {
      folders:
        cloneArray(docker.folders) ??
        cloneArray(defaultConfig.docker.folders) ??
        [],
      proxy: {
        caddyEnabled:
          docker.proxy?.caddyEnabled ?? defaultConfig.docker.proxy.caddyEnabled,
        baseDomain:
          docker.proxy?.baseDomain ?? defaultConfig.docker.proxy.baseDomain,
        tlsEmail: docker.proxy?.tlsEmail ?? defaultConfig.docker.proxy.tlsEmail,
      },
    },
    jobs: {
      ...defaultConfig.jobs,
      ...jobs,
    },
    dismissals: cfg?.dismissals ? { ...cfg.dismissals } : undefined,
  };
};

const mergeConfig = (prev: AppConfig, patch: ConfigPatch): AppConfig =>
  applyDefaults({
    appSettings: patch.appSettings
      ? { ...prev.appSettings, ...patch.appSettings }
      : prev.appSettings,
    docker: patch.docker
      ? {
          ...prev.docker,
          ...patch.docker,
          proxy: patch.docker.proxy
            ? { ...prev.docker.proxy, ...patch.docker.proxy }
            : prev.docker.proxy,
        }
      : prev.docker,
    jobs: patch.jobs ? { ...prev.jobs, ...patch.jobs } : prev.jobs,
    dismissals:
      patch.dismissals === undefined
        ? prev.dismissals
        : { ...prev.dismissals, ...patch.dismissals },
  });

const getConfigValue = <K extends ConfigValueKey>(
  cfg: AppConfig,
  key: K,
): ConfigValueMap[K] => {
  return cfg.appSettings[key as keyof AppSettings] as ConfigValueMap[K];
};

const patchConfigValue = <K extends ConfigValueKey>(
  key: K,
  value: ConfigValueMap[K],
): ConfigPatch => {
  return {
    appSettings: {
      [key]: value,
    } as Partial<AppSettings>,
  };
};

export const ConfigContext = createContext<ConfigContextType | undefined>(
  undefined,
);

export const ConfigProvider: React.FC<ConfigProviderProps> = ({ children }) => {
  const { signOut, user } = useAuth();
  const username = user?.id;
  const [config, setConfig] = useState<AppConfig>(() =>
    applyDefaults(readConfigCache(username)),
  );
  const [isLoaded, setLoaded] = useState(false);
  // Track if we successfully loaded from backend - only allow saves if true
  const [canSave, setCanSave] = useState(false);
  const queryClient = useQueryClient();
  const { mutate: setConfigRemote } = linuxio.config.set.useMutation({
    onSuccess: (_result, patch) => {
      if (patch.docker?.folders === undefined) return;
      void queryClient.invalidateQueries({
        queryKey: linuxio.docker.list_compose_projects.queryKey(),
      });
    },
  });

  useEffect(() => {
    let cancelled = false;
    let retryTimeout: ReturnType<typeof setTimeout> | null = null;

    const fetchConfig = async (attempt = 1): Promise<void> => {
      if (cancelled) return;

      try {
        // Wait for stream mux to be ready (250ms per attempt, up to 5 attempts = 2.5s total)
        const muxReady = await waitForStreamMux(250);
        if (cancelled) return;

        if (!muxReady) {
          // Mux not ready - retry quickly (100ms delay between attempts)
          if (attempt < 5) {
            retryTimeout = setTimeout(() => fetchConfig(attempt + 1), 100);
            return;
          }
          // After 5 attempts, use cached/default config but don't allow saving
          console.warn("Stream mux not ready, using cached/default config");
          setLoaded(true);
          // canSave stays false - prevent overwriting backend config with defaults
          return;
        }

        if (readConfigCache(username)) {
          setCanSave(true);
          setLoaded(true);
          return;
        }

        const settings = await queryClient.fetchQuery<AppConfig>(
          linuxio.config.get.queryOptions({ staleTime: CACHE_TTL_MS.NONE }),
        );

        if (!cancelled) {
          const nextConfig = applyDefaults(settings);
          setConfig(nextConfig);
          writeConfigCache(username, nextConfig);
          setCanSave(true); // Successfully loaded from backend, allow saves
          setLoaded(true);
        }
      } catch (error: unknown) {
        if (cancelled) return;

        // Don't treat stream errors as auth errors - just use defaults
        if (error instanceof LinuxIOError && error.code === 503) {
          console.warn("Stream API unavailable, using default config");
          setLoaded(true);
          // canSave stays false
          return;
        }

        // Only treat actual auth errors (401/403) as session expired
        const code = error instanceof LinuxIOError ? error.code : 500;
        if (code === 401 || code === 403) {
          toast.error("Session expired. Please sign in again.");
          await signOut();
          return;
        }

        // For other errors, just log and use defaults
        console.error("Failed to load config:", error);
        setLoaded(true);
        // canSave stays false
      }
    };

    // One-shot async config load (stream mux + react-query fetchQuery), not a
    // synchronous external store — useSyncExternalStore can't express async
    // loading, so this rule misfires here.
    // eslint-disable-next-line react-you-might-not-need-an-effect/no-external-store-subscription
    fetchConfig();

    return () => {
      cancelled = true;
      if (retryTimeout) clearTimeout(retryTimeout);
    };
  }, [queryClient, signOut, username]);

  const save = useCallback(
    (patch: ConfigPatch) => {
      if (!canSave) return; // Only save if we successfully loaded from backend
      setConfigRemote(patch);
    },
    [canSave, setConfigRemote],
  );

  const setKey: ConfigContextType["setKey"] = useCallback(
    (key, value) => {
      setConfig((prev) => {
        const current = getConfigValue(prev, key);
        const nextVal =
          typeof value === "function" ? (value as any)(current) : value;
        if (Object.is(current, nextVal)) return prev;
        const patch = patchConfigValue(key, nextVal);
        const next = mergeConfig(prev, patch);
        if (canSave) writeConfigCache(username, next);
        save(patch);
        return next;
      });
    },
    [canSave, save, username],
  );

  const updateConfig: ConfigContextType["updateConfig"] = useCallback(
    (patch) => {
      setConfig((prev) => {
        const partial = typeof patch === "function" ? patch(prev) : patch;
        const next = mergeConfig(prev, partial);
        if (canSave) writeConfigCache(username, next);
        save(partial);
        return next;
      });
    },
    [canSave, save, username],
  );

  const value = useMemo(
    () => ({ config, setKey, updateConfig, isLoaded }),
    [config, setKey, updateConfig, isLoaded],
  );
  if (!isLoaded) return null;
  return (
    <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
  );
};
