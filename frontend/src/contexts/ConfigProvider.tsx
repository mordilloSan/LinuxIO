// src/contexts/ConfigProvider.tsx
import { useQueryClient } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import {
  type AppConfig,
  type AppSettings,
  CACHE_TTL_MS,
  type ConfigDockerDashboardSections as DockerDashboardSections,
  type ConfigHardwareSections as HardwareSections,
  type ConfigThemeColorsByModePayload as ThemeColorsByMode,
  type ConfigThemeColorsPayload as ThemeColors,
  linuxio,
  LinuxIOError,
  type TableCardViewMode,
  useStreamMux,
  useCallMutation,
} from "@/api";
import {
  ConfigAccessorContext,
  ConfigContext,
  type ConfigAccessorContextValue,
} from "@/contexts/ConfigContext";
import useAuth from "@/hooks/useAuth";
import { useLatestRef } from "@/hooks/useLatestRef";
import type {
  ConfigContextType,
  ConfigPatch,
  ConfigProviderProps,
  ConfigValueKey,
  ConfigValueMap,
} from "@/types/config";
import { readConfigCache, writeConfigCache } from "@/utils/configCache";

type AppViewModes = Record<string, TableCardViewMode>;

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
    navigationMode: "sidebar",
    dockTileColors: "accent",
    showHiddenFiles: true,
    hiddenCards: [],
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
    requireMountsForFolders: false,
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

// Surfaces the user never rearranged stay absent instead of being stored as an
// empty array, so an undefined map is the normal case rather than a defect.
const cloneLayoutOrders = (
  layoutOrders?: AppSettings["layoutOrders"],
): AppSettings["layoutOrders"] => {
  if (!layoutOrders) return undefined;

  const normalized: Record<string, string[]> = {};
  for (const [surface, order] of Object.entries(layoutOrders)) {
    if (!surface || !Array.isArray(order) || order.length === 0) continue;
    normalized[surface] = [...order];
  }

  return Object.keys(normalized).length > 0 ? normalized : undefined;
};

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
      navigationMode:
        app.navigationMode ?? defaultConfig.appSettings.navigationMode,
      dockTileColors:
        app.dockTileColors ?? defaultConfig.appSettings.dockTileColors,
      showHiddenFiles:
        app.showHiddenFiles ?? defaultConfig.appSettings.showHiddenFiles,
      hiddenCards:
        cloneArray(app.hiddenCards) ??
        cloneArray(defaultConfig.appSettings.hiddenCards),
      layoutOrders: cloneLayoutOrders(app.layoutOrders),
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
      // Absent means "frontend default"; the terminal owns that constant.
      terminalFontSize: app.terminalFontSize,
    },
    docker: {
      folders:
        cloneArray(docker.folders) ??
        cloneArray(defaultConfig.docker.folders) ??
        [],
      requireMountsForFolders:
        docker.requireMountsForFolders ??
        defaultConfig.docker.requireMountsForFolders,
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

const mergeConfig = (prev: AppConfig, patch: ConfigPatch): AppConfig => {
  const next = applyDefaults({
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
  // applyDefaults defensively clones themeColors, which would hand memo
  // consumers (the theme provider) a fresh reference on every unrelated
  // update; keep the previous object when the patch didn't touch it.
  if (!patch.appSettings || !("themeColors" in patch.appSettings)) {
    next.appSettings.themeColors = prev.appSettings.themeColors;
  }
  return next;
};

const getConfigValue = <K extends ConfigValueKey>(
  cfg: AppConfig,
  key: K,
): ConfigValueMap[K] => {
  return cfg.appSettings[key];
};

const patchConfigValue = <K extends ConfigValueKey>(
  key: K,
  value: ConfigValueMap[K],
): ConfigPatch => {
  return {
    appSettings: {
      [key]: value,
    },
  };
};

// Config state is deliberately layered: the sessionStorage cache seeds the
// initial render synchronously (no theme flash before the mux is up), the
// useState mirror is the live copy feature code reads, and the backend is the
// durable store. Saves are optimistic — local state updates immediately and a
// failed persist surfaces via the action's error toast.
export const ConfigProvider = ({ children }: ConfigProviderProps) => {
  const { sessionExpired, user } = useAuth();
  const queryClient = useQueryClient();
  const username = user?.id;
  const [config, setConfig] = useState<AppConfig>(() =>
    applyDefaults(readConfigCache(username)),
  );
  const [isLoaded, setLoaded] = useState(false);
  // Track if we successfully loaded from backend - only allow saves if true
  const [canSave, setCanSave] = useState(false);
  // Keep a synchronously updated draft so multiple actions in one event use
  // the latest state even before React commits the queued update.
  const configDraftRef = useRef(config);
  useLayoutEffect(() => {
    configDraftRef.current = config;
  }, [config]);
  const { isOpen: isMuxOpen } = useStreamMux();
  const { mutate: setConfigRemote } = useCallMutation(linuxio.config.set, {
    error: "Failed to save settings",
    invalidates: (_result, patch) =>
      patch.docker?.folders !== undefined
        ? [linuxio.docker.list_compose_projects.queryKey]
        : [],
  });

  useEffect(() => {
    let cancelled = false;
    // If the mux never opens, render with cached/default config after 2.5s;
    // saving stays disabled until a backend load succeeds.
    const giveUp = setTimeout(() => {
      if (!cancelled) {
        console.warn("Stream mux not ready, using cached/default config");
        setLoaded(true);
      }
    }, 2_500);

    const fetchConfig = async (): Promise<void> => {
      // The effect re-runs when the mux opens (same gating the endpoint
      // hooks get from `enabled`), so a slow startup never leaves saving
      // disabled forever.
      if (!isMuxOpen) return;

      const loadConfig = async () => {
        try {
          if (readConfigCache(username)) {
            setCanSave(true);
            setLoaded(true);
            return;
          }

          const settings = await queryClient.fetchQuery({
            ...linuxio.config.get,
            staleTime: CACHE_TTL_MS.NONE,
          });

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
            // Involuntary expiry during config load: preserve the path, notify,
            // and sign out locally (same handling as a dropped auth socket).
            sessionExpired();
            return;
          }

          // For other errors, just log and use defaults
          console.error("Failed to load config:", error);
          setLoaded(true);
          // canSave stays false
        }
      };

      await loadConfig().finally(() => {
        // The load settled one way or another; the give-up fallback must not
        // fire later and warn about a mux that is actually up.
        clearTimeout(giveUp);
      });
    };

    // Async config load (mux-gated, query-cache fetcher), not a synchronous
    // external store — useSyncExternalStore can't express async loading, so
    // this rule misfires here.

    void fetchConfig();

    return () => {
      cancelled = true;
      clearTimeout(giveUp);
    };
  }, [isMuxOpen, queryClient, sessionExpired, username]);

  // Warn once per unreachable period, not on every discarded change.
  const warnedUnsavedRef = useRef(false);
  const save = useCallback(
    (patch: ConfigPatch, onSaved?: () => void) => {
      if (!canSave) {
        // Startup never reached the backend: the change stays local so the
        // UI keeps working, but the user must know it will not survive.
        if (!warnedUnsavedRef.current) {
          warnedUnsavedRef.current = true;
          toast.warning("Settings are not being saved (backend unreachable).");
        }
        return;
      }
      warnedUnsavedRef.current = false;
      if (onSaved) {
        setConfigRemote(patch, { onSuccess: onSaved });
      } else {
        setConfigRemote(patch);
      }
    },
    [canSave, setConfigRemote],
  );

  const setKey: ConfigContextType["setKey"] = useCallback(
    (key, value) => {
      const prev = configDraftRef.current;
      const current = getConfigValue(prev, key);
      const nextVal =
        typeof value === "function" ? (value as any)(current) : value;
      if (Object.is(current, nextVal)) return;
      const patch = patchConfigValue(key, nextVal);
      const next = mergeConfig(prev, patch);
      configDraftRef.current = next;
      setConfig(next);
      if (canSave) writeConfigCache(username, next);
      save(patch);
    },
    [canSave, save, username],
  );

  const updateConfig: ConfigContextType["updateConfig"] = useCallback(
    (patch, onSaved) => {
      const prev = configDraftRef.current;
      const partial = typeof patch === "function" ? patch(prev) : patch;
      const next = mergeConfig(prev, partial);
      configDraftRef.current = next;
      setConfig(next);
      if (canSave) writeConfigCache(username, next);
      save(partial, onSaved);
    },
    [canSave, save, username],
  );

  const value = useMemo(
    () => ({ config, setKey, updateConfig, isLoaded }),
    [config, setKey, updateConfig, isLoaded],
  );
  const configRef = useLatestRef(config);
  const accessorValue = useMemo<ConfigAccessorContextValue>(
    () => ({ getConfig: () => configRef.current }),
    [configRef],
  );
  if (!isLoaded) return null;
  return (
    <ConfigAccessorContext.Provider value={accessorValue}>
      <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
    </ConfigAccessorContext.Provider>
  );
};
