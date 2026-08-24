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
  bridgeConfigQueryKey,
  isBridgeAppSettingKey,
  pruneViewModes,
  uiConfigQueryKey,
} from "@/api/config-query";
import { ConfigContext } from "@/contexts/ConfigContext";
import useAuth from "@/hooks/useAuth";
import type {
  ConfigContextType,
  ConfigPatch,
  ConfigProviderProps,
  ConfigValueKey,
  ConfigValueMap,
  EffectiveAppConfig,
  EffectiveUISettings,
} from "@/types/config";

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

function pickImportantAppSettings(
  appSettings: Partial<ConfigValueMap>,
): Partial<AppSettings> {
  const result: Partial<AppSettings> = {};
  for (const key of Object.keys(appSettings)) {
    if (isBridgeAppSettingKey(key)) {
      Object.assign(result, { [key]: appSettings[key as ConfigValueKey] });
    }
  }
  return result;
}

function pickUISettings(
  appSettings: Partial<ConfigValueMap> | undefined,
): Partial<EffectiveUISettings> {
  const result: Partial<EffectiveUISettings> = {};
  if (!appSettings) return result;
  for (const key of Object.keys(appSettings)) {
    if (!isBridgeAppSettingKey(key)) {
      Object.assign(result, { [key]: appSettings[key as ConfigValueKey] });
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

function hasValues(value: object): boolean {
  return Object.keys(value).length > 0;
}

function hasImportantPatch(patch: ConfigPatch): boolean {
  return hasValues(bridgePatch(patch));
}

function hasUIPatch(patch: ConfigPatch): boolean {
  return hasValues(pickUISettings(patch.appSettings));
}

/**
 * Full replacement payload for config.set_ui from the cached UI snapshot.
 * Absent optional values (themeColors) are omitted, never sent as undefined.
 */
function buildUIPayload(ui: UIConfig): ConfigUISetPayload {
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
    viewModes: { ...pruneViewModes(ui.viewModes, ui.viewModeDefault) },
    terminalFontSize: ui.terminalFontSize,
    layoutOrders: Object.fromEntries(
      Object.entries(ui.layoutOrders).map(([key, order]) => [key, [...order]]),
    ),
  };
  if (ui.themeColors) {
    payload.themeColors = cloneThemeColorsByMode(ui.themeColors);
  }
  return payload;
}

function mergeBridgeConfig(previous: AppConfig, patch: ConfigPatch): AppConfig {
  return {
    appSettings: {
      ...previous.appSettings,
      ...pickImportantAppSettings(patch.appSettings ?? {}),
    },
    docker: patch.docker
      ? {
          ...previous.docker,
          ...patch.docker,
          proxy: patch.docker.proxy
            ? { ...previous.docker.proxy, ...patch.docker.proxy }
            : previous.docker.proxy,
        }
      : previous.docker,
    jobs: patch.jobs ? { ...previous.jobs, ...patch.jobs } : previous.jobs,
    dismissals:
      patch.dismissals === undefined
        ? previous.dismissals
        : { ...previous.dismissals, ...patch.dismissals },
  };
}

function composeEffectiveConfig(
  bridge: AppConfig,
  ui: UIConfig,
): EffectiveAppConfig {
  return {
    ...bridge,
    appSettings: {
      ...bridge.appSettings,
      ...ui,
      viewModes: pruneViewModes(ui.viewModes, ui.viewModeDefault),
    },
  };
}

function readConfigValue(
  bridge: AppConfig,
  ui: UIConfig,
  key: ConfigValueKey,
): unknown {
  if (isBridgeAppSettingKey(key)) return bridge.appSettings[key];
  if (key === "viewModes") {
    return pruneViewModes(ui.viewModes, ui.viewModeDefault);
  }
  return ui[key as keyof UIConfig];
}

interface SaveOperation {
  bridge?: ConfigPatch;
  ui?: ConfigUISetPayload;
  onSaved?: () => void;
}

export const ConfigProvider = ({ children }: ConfigProviderProps) => {
  const { sessionExpired, user } = useAuth();
  const queryClient = useQueryClient();
  const [isLoaded, setLoaded] = useState(false);
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
  const configUserId = user?.id ?? "anonymous";
  const bridgeKey = useMemo(
    () => bridgeConfigQueryKey(configUserId),
    [configUserId],
  );
  const uiKey = useMemo(() => uiConfigQueryKey(configUserId), [configUserId]);

  useEffect(() => {
    activeQueueRef.current = true;
    return () => {
      activeQueueRef.current = false;
      canSaveRef.current = false;
    };
  }, []);

  useEffect(() => {
    return () => {
      queryClient.removeQueries({ exact: true, queryKey: bridgeKey });
      queryClient.removeQueries({ exact: true, queryKey: uiKey });
    };
  }, [queryClient, bridgeKey, uiKey]);

  const enqueueSave = useCallback(
    (operation: SaveOperation) => {
      saveTailRef.current = saveTailRef.current
        .then(async () => {
          if (!activeQueueRef.current) return;
          const results = await Promise.allSettled([
            operation.bridge
              ? setConfigRemote(operation.bridge)
              : Promise.resolve(),
            operation.ui ? setUIRemote(operation.ui) : Promise.resolve(),
          ]);
          if (!activeQueueRef.current) return;
          if (results.some((result) => result.status === "rejected")) return;
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
      const cachedUI = queryClient.getQueryData<UIConfig>(uiKey);
      const results = await Promise.allSettled([
        queryClient.query({
          ...linuxio.config.get,
          queryKey: bridgeKey,
          staleTime: CACHE_TTL_MS.NONE,
        }),
        cachedUI
          ? Promise.resolve(cachedUI)
          : queryClient.query({
              ...linuxio.config.get_ui,
              queryKey: uiKey,
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
  }, [isMuxOpen, queryClient, bridgeKey, uiKey, sessionExpired]);

  const save = useCallback(
    (patch: ConfigPatch, nextUI: UIConfig, onSaved?: () => void) => {
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
        ui: wantsUI ? buildUIPayload(nextUI) : undefined,
        onSaved,
      });
    },
    [enqueueSave],
  );

  /**
   * Optimistically apply a patch to the two cache snapshots — the query cache
   * is the single frontend copy that every slice hook subscribes to. Returns
   * the post-patch UI snapshot so the save payload reflects this exact state.
   */
  const applyPatch = useCallback(
    (patch: ConfigPatch): { ui: UIConfig } | null => {
      const bridge = queryClient.getQueryData<AppConfig>(bridgeKey);
      const ui = queryClient.getQueryData<UIConfig>(uiKey);
      if (!bridge || !ui) return null;

      const uiPart = pickUISettings(patch.appSettings);
      const nextUI: UIConfig = hasValues(uiPart) ? { ...ui, ...uiPart } : ui;
      if (nextUI !== ui) queryClient.setQueryData(uiKey, nextUI);

      if (
        hasValues(pickImportantAppSettings(patch.appSettings ?? {})) ||
        patch.docker !== undefined ||
        patch.jobs !== undefined ||
        patch.dismissals !== undefined
      ) {
        queryClient.setQueryData(bridgeKey, mergeBridgeConfig(bridge, patch));
      }
      return { ui: nextUI };
    },
    [queryClient, bridgeKey, uiKey],
  );

  const setKey: ConfigContextType["setKey"] = useCallback(
    (key, value) => {
      const bridge = queryClient.getQueryData<AppConfig>(bridgeKey);
      const ui = queryClient.getQueryData<UIConfig>(uiKey);
      if (!bridge || !ui) return;
      type Value = ConfigValueMap[typeof key];
      const current = readConfigValue(bridge, ui, key) as Value;
      const nextValue = typeof value === "function" ? value(current) : value;
      if (Object.is(current, nextValue)) return;

      const patch: ConfigPatch = { appSettings: { [key]: nextValue } };
      const applied = applyPatch(patch);
      if (!applied) return;
      save(patch, applied.ui);
    },
    [queryClient, bridgeKey, uiKey, applyPatch, save],
  );

  const updateConfig: ConfigContextType["updateConfig"] = useCallback(
    (patch, onSaved) => {
      const bridge = queryClient.getQueryData<AppConfig>(bridgeKey);
      const ui = queryClient.getQueryData<UIConfig>(uiKey);
      if (!bridge || !ui) return;
      const resolvedPatch =
        typeof patch === "function"
          ? patch(composeEffectiveConfig(bridge, ui))
          : patch;
      const applied = applyPatch(resolvedPatch);
      if (!applied) return;
      save(resolvedPatch, applied.ui, onSaved);
    },
    [queryClient, bridgeKey, uiKey, applyPatch, save],
  );

  const value = useMemo<ConfigContextType>(
    () => ({ isLoaded: true, setKey, updateConfig }),
    [setKey, updateConfig],
  );

  if (!isLoaded) return null;
  return (
    <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
  );
};
