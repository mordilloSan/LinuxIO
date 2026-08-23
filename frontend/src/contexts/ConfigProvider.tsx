import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  type AppConfig,
  type AppSettings,
  CACHE_TTL_MS,
  type ConfigUISetPayload,
  type ConfigThemeColorsByModePayload as ThemeColorsByMode,
  type ConfigThemeColorsPayload as ThemeColors,
  type UIConfig,
  linuxio,
  LinuxIOError,
  useCallMutation,
  useStreamMux,
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
  EffectiveAppConfig,
  EffectiveAppSettings,
  EffectiveUISettings,
} from "@/types/config";

type ConfigurableUIKey = Exclude<keyof EffectiveUISettings, "viewModeDefault">;

const IMPORTANT_APP_KEYS = ["showHiddenFiles", "chunkSizeMB"] as const;

const UI_KEYS = [
  "theme",
  "primaryColor",
  "themeColors",
  "sidebarCollapsed",
  "navigationMode",
  "dockTileColors",
  "dockAccentGradient",
  "hiddenCards",
  "dockerDashboardSections",
  "hardwareSections",
  "viewModes",
  "layoutOrders",
  "terminalFontSize",
] as const satisfies readonly ConfigurableUIKey[];

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

function cloneUIConfig(value: UIConfig): EffectiveUISettings {
  return {
    theme: value.theme,
    primaryColor: value.primaryColor,
    themeColors: cloneThemeColorsByMode(value.themeColors),
    sidebarCollapsed: value.sidebarCollapsed,
    navigationMode: value.navigationMode,
    dockTileColors: value.dockTileColors,
    dockAccentGradient: { ...value.dockAccentGradient },
    hiddenCards: [...value.hiddenCards],
    dockerDashboardSections: { ...value.dockerDashboardSections },
    hardwareSections: { ...value.hardwareSections },
    viewModes: { ...value.viewModes },
    viewModeDefault: value.viewModeDefault,
    layoutOrders: Object.fromEntries(
      Object.entries(value.layoutOrders).map(([key, order]) => [
        key,
        [...order],
      ]),
    ),
    terminalFontSize: value.terminalFontSize,
  };
}

function cloneAppConfig(value: AppConfig): AppConfig {
  return {
    appSettings: { ...value.appSettings },
    docker: {
      ...value.docker,
      folders: [...value.docker.folders],
      proxy: { ...value.docker.proxy },
    },
    jobs: { ...value.jobs },
    dismissals: value.dismissals ? { ...value.dismissals } : undefined,
  };
}

function composeConfig(bridge: AppConfig, ui: UIConfig): EffectiveAppConfig {
  const clonedBridge = cloneAppConfig(bridge);
  return {
    ...clonedBridge,
    appSettings: {
      ...clonedBridge.appSettings,
      ...cloneUIConfig(ui),
    },
  };
}

function cloneEffectiveAppSettings(
  value: EffectiveAppSettings,
): EffectiveAppSettings {
  return {
    ...value,
    themeColors: cloneThemeColorsByMode(value.themeColors),
    dockAccentGradient: { ...value.dockAccentGradient },
    hiddenCards: [...value.hiddenCards],
    dockerDashboardSections: { ...value.dockerDashboardSections },
    hardwareSections: { ...value.hardwareSections },
    viewModes: { ...value.viewModes },
    layoutOrders: Object.fromEntries(
      Object.entries(value.layoutOrders).map(([key, order]) => [
        key,
        [...order],
      ]),
    ),
  };
}

function mergeConfig(
  previous: EffectiveAppConfig,
  patch: ConfigPatch,
): EffectiveAppConfig {
  const nextAppSettings = cloneEffectiveAppSettings({
    ...previous.appSettings,
    ...(patch.appSettings ?? {}),
  });
  return {
    ...previous,
    appSettings: nextAppSettings,
    docker: patch.docker
      ? {
          ...previous.docker,
          ...patch.docker,
          folders:
            patch.docker.folders === undefined
              ? [...previous.docker.folders]
              : [...patch.docker.folders],
          proxy: patch.docker.proxy
            ? { ...previous.docker.proxy, ...patch.docker.proxy }
            : { ...previous.docker.proxy },
        }
      : previous.docker,
    jobs: patch.jobs ? { ...previous.jobs, ...patch.jobs } : previous.jobs,
    dismissals:
      patch.dismissals === undefined
        ? previous.dismissals
        : { ...previous.dismissals, ...patch.dismissals },
  };
}

function pickImportantAppSettings(
  appSettings: Partial<EffectiveAppSettings>,
): Partial<AppSettings> {
  const result: Partial<AppSettings> = {};
  for (const key of IMPORTANT_APP_KEYS) {
    if (key in appSettings) {
      Object.assign(result, { [key]: appSettings[key] });
    }
  }
  return result;
}

function pickUISettings(
  appSettings: Partial<EffectiveAppSettings> | undefined,
): Partial<EffectiveUISettings> {
  const result: Partial<EffectiveUISettings> = {};
  if (!appSettings) return result;
  for (const key of UI_KEYS) {
    if (key in appSettings) {
      Object.assign(result, { [key]: appSettings[key] });
    }
  }
  return result;
}

function bridgePatch(patch: ConfigPatch): ConfigPatch {
  const result: ConfigPatch = {};
  const appSettings = pickImportantAppSettings(patch.appSettings ?? {});
  if (Object.keys(appSettings).length > 0) result.appSettings = appSettings;
  if (patch.docker !== undefined) result.docker = patch.docker;
  if (patch.jobs !== undefined) result.jobs = patch.jobs;
  if (patch.dismissals !== undefined) result.dismissals = patch.dismissals;
  return result;
}

function uiPatch(patch: ConfigPatch): Partial<EffectiveUISettings> {
  return pickUISettings(patch.appSettings);
}

function hasValues(value: object): boolean {
  return Object.keys(value).length > 0;
}

function hasImportantPatch(patch: ConfigPatch): boolean {
  return hasValues(bridgePatch(patch));
}

function hasUIPatch(patch: ConfigPatch): boolean {
  return hasValues(uiPatch(patch));
}

function uiSnapshotFromConfig(config: EffectiveAppConfig): ConfigUISetPayload {
  const ui = config.appSettings;
  const payload: ConfigUISetPayload = {
    theme: ui.theme,
    primaryColor: ui.primaryColor,
    sidebarCollapsed: ui.sidebarCollapsed,
    navigationMode: ui.navigationMode,
    dockTileColors: ui.dockTileColors,
    dockAccentGradient: { ...ui.dockAccentGradient },
    hiddenCards: [...ui.hiddenCards],
    dockerDashboardSections: { ...ui.dockerDashboardSections },
    hardwareSections: { ...ui.hardwareSections },
    viewModes: { ...ui.viewModes },
    terminalFontSize: ui.terminalFontSize,
  };
  if (ui.themeColors)
    payload.themeColors = cloneThemeColorsByMode(ui.themeColors);
  payload.layoutOrders = Object.fromEntries(
    Object.entries(ui.layoutOrders).map(([key, order]) => [key, [...order]]),
  );
  return payload;
}

const getConfigValue = <K extends ConfigValueKey>(
  config: EffectiveAppConfig,
  key: K,
): ConfigValueMap[K] => config.appSettings[key];

const patchConfigValue = <K extends ConfigValueKey>(
  key: K,
  value: ConfigValueMap[K],
): ConfigPatch => ({ appSettings: { [key]: value } });

interface SaveOperation {
  bridge?: ConfigPatch;
  ui?: ConfigUISetPayload;
  onSaved?: () => void;
}

function LoadedConfigProviders({
  children,
  config,
  setKey,
  updateConfig,
}: Pick<ConfigProviderProps, "children"> &
  Pick<ConfigContextType, "config" | "setKey" | "updateConfig">) {
  const configRef = useLatestRef(config);
  const value = useMemo<ConfigContextType>(
    () => ({ config, setKey, updateConfig, isLoaded: true }),
    [config, setKey, updateConfig],
  );
  const accessorValue = useMemo<ConfigAccessorContextValue>(
    () => ({ getConfig: () => configRef.current }),
    [configRef],
  );

  return (
    <ConfigAccessorContext.Provider value={accessorValue}>
      <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
    </ConfigAccessorContext.Provider>
  );
}

export const ConfigProvider = ({ children }: ConfigProviderProps) => {
  const { sessionExpired } = useAuth();
  const queryClient = useQueryClient();
  const [config, setConfig] = useState<EffectiveAppConfig | null>(null);
  const [isLoaded, setLoaded] = useState(false);
  const configDraftRef = useRef<EffectiveAppConfig | null>(null);
  const loadedOnceRef = useRef(false);
  const canSaveRef = useRef(false);
  const warnedUnsavedRef = useRef(false);
  const { isOpen: isMuxOpen } = useStreamMux();
  const { mutateAsync: setConfigRemote } = useCallMutation(linuxio.config.set, {
    error: "Failed to save settings",
    invalidates: (_result, patch) =>
      patch.docker?.folders !== undefined
        ? [linuxio.docker.list_compose_projects.queryKey]
        : [],
  });
  const { mutateAsync: setUIRemote } = useCallMutation(linuxio.config.set_ui, {
    error: "Failed to save appearance settings",
    invalidates: [],
  });
  const saveTailRef = useRef<Promise<void>>(Promise.resolve());
  const activeQueueRef = useRef(true);

  useEffect(() => {
    activeQueueRef.current = true;
    return () => {
      activeQueueRef.current = false;
      canSaveRef.current = false;
    };
  }, []);

  const enqueueSave = useCallback(
    (operation: SaveOperation) => {
      saveTailRef.current = saveTailRef.current
        .then(async () => {
          if (!activeQueueRef.current) return;
          if (operation.bridge) {
            await setConfigRemote(operation.bridge);
            if (!activeQueueRef.current) return;
          }
          if (operation.ui) {
            await setUIRemote(operation.ui);
            if (!activeQueueRef.current) return;
          }
          operation.onSaved?.();
        })
        // Mutation hooks report the individual failure. Keep the queue alive
        // so a later user change can still be sent.
        .catch(() => undefined);
    },
    [setConfigRemote, setUIRemote],
  );

  useEffect(() => {
    let cancelled = false;
    canSaveRef.current = false;

    if (!isMuxOpen) {
      return () => {
        cancelled = true;
      };
    }

    const load = async () => {
      const results = await Promise.allSettled([
        queryClient.fetchQuery({
          ...linuxio.config.get,
          staleTime: CACHE_TTL_MS.NONE,
        }),
        queryClient.fetchQuery({
          ...linuxio.config.get_ui,
          staleTime: CACHE_TTL_MS.NONE,
        }),
      ]);
      if (cancelled) return;

      for (const result of results) {
        if (result.status !== "rejected") continue;
        const error = result.reason;
        const code = error instanceof LinuxIOError ? error.code : 500;
        if (code === 401 || code === 403) {
          sessionExpired();
          return;
        }
      }

      const bridgeResult = results[0];
      const uiResult = results[1];
      if (
        bridgeResult.status !== "fulfilled" ||
        uiResult.status !== "fulfilled"
      ) {
        if (!loadedOnceRef.current) setLoaded(false);
        if (bridgeResult.status === "rejected") {
          console.error("Failed to load config:", bridgeResult.reason);
        }
        if (uiResult.status === "rejected") {
          console.error("Failed to load appearance settings:", uiResult.reason);
        }
        return;
      }

      const bridge = bridgeResult.value;
      const ui = uiResult.value;
      const nextConfig = composeConfig(bridge, ui);
      configDraftRef.current = nextConfig;
      setConfig(nextConfig);
      loadedOnceRef.current = true;
      setLoaded(true);
      canSaveRef.current = true;
      warnedUnsavedRef.current = false;
    };

    void load();
    return () => {
      cancelled = true;
      canSaveRef.current = false;
    };
  }, [isMuxOpen, queryClient, sessionExpired]);

  const save = useCallback(
    (
      patch: ConfigPatch,
      nextConfig: EffectiveAppConfig,
      onSaved?: () => void,
    ) => {
      const wantsBridge = hasImportantPatch(patch);
      const wantsUI = hasUIPatch(patch);
      if (!wantsBridge && !wantsUI) return;

      if (!canSaveRef.current) {
        if (!warnedUnsavedRef.current) {
          warnedUnsavedRef.current = true;
          toast.warning("Settings are not being saved (backend unreachable).");
        }
        return;
      }

      warnedUnsavedRef.current = false;
      enqueueSave({
        bridge: wantsBridge ? bridgePatch(patch) : undefined,
        ui: wantsUI ? uiSnapshotFromConfig(nextConfig) : undefined,
        onSaved,
      });
    },
    [enqueueSave],
  );

  const setKey: ConfigContextType["setKey"] = useCallback(
    (key, value) => {
      const previous = configDraftRef.current;
      if (!previous) return;
      const current = getConfigValue(previous, key);
      const nextValue =
        typeof value === "function" ? (value as any)(current) : value;
      if (Object.is(current, nextValue)) return;

      const patch = patchConfigValue(key, nextValue);
      const nextConfig = mergeConfig(previous, patch);
      configDraftRef.current = nextConfig;
      setConfig(nextConfig);
      save(patch, nextConfig);
    },
    [save],
  );

  const updateConfig: ConfigContextType["updateConfig"] = useCallback(
    (patch, onSaved) => {
      const previous = configDraftRef.current;
      if (!previous) return;
      const resolvedPatch =
        typeof patch === "function" ? patch(previous) : patch;
      const nextConfig = mergeConfig(previous, resolvedPatch);
      configDraftRef.current = nextConfig;
      setConfig(nextConfig);
      save(resolvedPatch, nextConfig, onSaved);
    },
    [save],
  );

  if (!config || !isLoaded) return null;
  return (
    <LoadedConfigProviders
      config={config}
      setKey={setKey}
      updateConfig={updateConfig}
    >
      {children}
    </LoadedConfigProviders>
  );
};
