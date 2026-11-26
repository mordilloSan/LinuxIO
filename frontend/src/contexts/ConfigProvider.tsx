import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  type AppConfig,
  type AppSettings,
  CACHE_TTL_MS,
  type ConfigThemeColorsByModePayload as ThemeColorsByMode,
  type ConfigThemeColorsPayload as ThemeColors,
  type ConfigUISetPayload,
  linuxio,
  LinuxIOError,
  type UIConfig,
  useStreamMux,
} from "@/api";
import type {
  ConfigContextType,
  ConfigPatch,
  ConfigProviderProps,
  ConfigValueKey,
  ConfigValueMap,
  EffectiveAppConfig,
  EffectiveUISettings,
} from "@/api";
import {
  bridgeConfigQueryKey,
  isBridgeAppSettingKey,
  pruneViewModes,
  uiConfigQueryKey,
} from "@/api/config-query";
import { cacheUserId } from "@/api/user-scope";
import { ConfigContext } from "@/contexts/ConfigContext";
import useAuth from "@/hooks/useAuth";

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

const cloneNameLists = (
  lists: Record<string, string[]>,
): Record<string, string[]> =>
  Object.fromEntries(
    Object.entries(lists).map(([key, names]) => [key, [...names]]),
  );

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
  if (patch.assistant !== undefined) result.assistant = patch.assistant;
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
 * Every setting config.set_ui stores except themeColors, which is omitted
 * while unset. The save replaces the stored settings, so each one named here
 * must be sent: leaving one out of buildUIPayload is a compile error.
 */
type UIPayloadSettings = Required<Omit<ConfigUISetPayload, "themeColors">>;

/**
 * Full replacement payload for config.set_ui from the cached UI snapshot.
 * Absent optional values (themeColors) are omitted, never sent as undefined.
 */
function buildUIPayload(ui: UIConfig): ConfigUISetPayload {
  const payload: UIPayloadSettings & ConfigUISetPayload = {
    theme: ui.theme,
    primaryColor: ui.primaryColor,
    sidebarCollapsed: ui.sidebarCollapsed,
    navigationMode: ui.navigationMode,
    dockTileColors: ui.dockTileColors,
    dockAccentGradient: { ...ui.dockAccentGradient },
    hiddenCards: [...ui.hiddenCards],
    collapsedSections: cloneNameLists(ui.collapsedSections),
    viewModes: { ...pruneViewModes(ui.viewModes, ui.viewModeDefault) },
    terminalFontSize: ui.terminalFontSize,
    assistantFontSize: ui.assistantFontSize,
    layoutOrders: cloneNameLists(ui.layoutOrders),
  };
  if (ui.themeColors) {
    payload.themeColors = cloneThemeColorsByMode(ui.themeColors);
  }
  return payload;
}

function mergeBridgeConfig(previous: AppConfig, patch: ConfigPatch): AppConfig {
  return {
    assistant: patch.assistant
      ? { ...previous.assistant, ...patch.assistant }
      : previous.assistant,
    appSettings: {
      ...previous.appSettings,
      ...pickImportantAppSettings(patch.appSettings ?? {}),
    },
    docker: patch.docker
      ? { ...previous.docker, ...patch.docker }
      : previous.docker,
    jobs: patch.jobs ? { ...previous.jobs, ...patch.jobs } : previous.jobs,
    dismissals:
      patch.dismissals === undefined
        ? previous.dismissals
        : { ...previous.dismissals, ...patch.dismissals },
    storageMode: previous.storageMode,
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

// Written after a confirmed UI save so the other tabs of this browser reload
// their settings. The value only has to change; nothing reads it.
const UI_CONFIG_SAVED_KEY = "linuxio.uiConfigSaved";

function announceUIConfigSaved(): void {
  try {
    window.localStorage.setItem(UI_CONFIG_SAVED_KEY, String(Date.now()));
  } catch {
    // Without storage the other tabs keep their settings until refreshed.
  }
}

const SERVER_SETTING_REFUSED =
  "Setting not saved: the server cannot be reached. Try again in a moment.";
const APPEARANCE_SAVED_LATER =
  "The server cannot be reached. Appearance changes are saved when it is back.";

// A failed load of the settings is tried again after 1 s, then 2 s, 4 s and
// so on up to 30 s, for as long as the connection stays open.
const LOAD_RETRY_BASE_MS = 1000;
const LOAD_RETRY_MAX_MS = 30_000;

/**
 * The part of a patch the page may show now. While nothing can be sent, a
 * bridge setting is refused with a notice instead of being shown: the next
 * load would take it back without a word, and it changes what the host does.
 * The appearance part stays, and is sent after that load.
 */
function sendablePart(patch: ConfigPatch, canSave: boolean): ConfigPatch {
  if (canSave || !hasImportantPatch(patch)) return patch;
  toast.warning(SERVER_SETTING_REFUSED);
  return { appSettings: pickUISettings(patch.appSettings) };
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
  // The cache holds an appearance change made while nothing could be sent.
  const unsentUIRef = useRef(false);
  const { isOpen: isMuxOpen } = useStreamMux();
  const { mutateAsync: setConfigRemote } = linuxio.config.set.useMutation({
    error: "Failed to save settings",
    invalidates: (_result, patch) =>
      patch.docker?.folders !== undefined
        ? [linuxio.docker.list_compose_projects.queryKey]
        : [],
  });
  const { mutateAsync: setUIRemote } = linuxio.config.set_ui.useMutation({
    error: "Failed to save appearance settings",
    invalidates: [],
  });
  const saveTailRef = useRef<Promise<void>>(Promise.resolve());
  const pendingSavesRef = useRef(0);
  // Stores whose cache holds a value the bridge refused to save.
  const refusedRef = useRef({ bridge: false, ui: false });
  const activeQueueRef = useRef(true);
  const configUserId = cacheUserId(user);
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
      unsentUIRef.current = false;
    };
  }, [queryClient, bridgeKey, uiKey]);

  const enqueueSave = useCallback(
    (operation: SaveOperation) => {
      // The cache already shows a refused value; read the refused stores back
      // so the page shows what the bridge holds. A store stays marked while
      // another save is queued, and is read back again after that save.
      const reloadRefused = async () => {
        const refused = refusedRef.current;
        if (!activeQueueRef.current || (!refused.bridge && !refused.ui)) {
          return;
        }
        const results = await Promise.allSettled([
          refused.bridge
            ? queryClient
                .query({
                  ...linuxio.config.get,
                  queryKey: bridgeKey,
                  staleTime: CACHE_TTL_MS.NONE,
                })
                .then(() => {
                  if (pendingSavesRef.current === 0) refused.bridge = false;
                })
            : undefined,
          refused.ui
            ? queryClient
                .query({
                  ...linuxio.config.get_ui,
                  queryKey: uiKey,
                  staleTime: CACHE_TTL_MS.NONE,
                })
                .then(() => {
                  if (pendingSavesRef.current === 0) refused.ui = false;
                })
            : undefined,
        ]);
        for (const result of results) {
          if (result.status !== "rejected") continue;
          const error = result.reason;
          const code = error instanceof LinuxIOError ? error.code : 500;
          if (code === 401 || code === 403) {
            sessionExpired();
            return;
          }
          // Otherwise the store stays marked and the next save reads it back.
        }
      };

      pendingSavesRef.current += 1;
      saveTailRef.current = saveTailRef.current
        .then(async () => {
          if (!activeQueueRef.current) return;
          const [bridgeResult, uiResult] = await Promise.allSettled([
            operation.bridge
              ? setConfigRemote(operation.bridge)
              : Promise.resolve(),
            operation.ui ? setUIRemote(operation.ui) : Promise.resolve(),
          ]);
          if (operation.ui && uiResult.status === "fulfilled") {
            announceUIConfigSaved();
          }
          if (!activeQueueRef.current) return;
          if (bridgeResult.status === "rejected") {
            refusedRef.current.bridge = true;
          }
          if (uiResult.status === "rejected") refusedRef.current.ui = true;
          if (
            bridgeResult.status === "rejected" ||
            uiResult.status === "rejected"
          ) {
            return;
          }
          operation.onSaved?.();
        })
        // Mutation hooks report the individual failure. Keep the queue alive
        // so a later user change can still be sent.
        .catch(() => undefined)
        .then(async () => {
          pendingSavesRef.current -= 1;
          if (pendingSavesRef.current === 0) await reloadRefused();
        })
        .catch(() => undefined);
    },
    [
      setConfigRemote,
      setUIRemote,
      queryClient,
      bridgeKey,
      uiKey,
      sessionExpired,
    ],
  );

  useEffect(() => {
    let cancelled = false;
    let retryTimer: number | undefined;
    let failedLoads = 0;
    canSaveRef.current = false;

    if (!isMuxOpen) {
      return () => {
        cancelled = true;
      };
    }

    const load = async () => {
      const cachedUI = queryClient.getQueryData<UIConfig>(uiKey);
      // The first failure is reported and retried once as any query is. The
      // attempts after it are quiet, and this loop does their waiting.
      const retrying =
        failedLoads > 0 ? { retry: false, meta: { silent: true } } : {};
      const results = await Promise.allSettled([
        queryClient.query({
          ...linuxio.config.get,
          queryKey: bridgeKey,
          staleTime: CACHE_TTL_MS.NONE,
          ...retrying,
        }),
        cachedUI
          ? Promise.resolve(cachedUI)
          : queryClient.query({
              ...linuxio.config.get_ui,
              queryKey: uiKey,
              staleTime: CACHE_TTL_MS.NONE,
              ...retrying,
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
        // Until a load succeeds nothing is saved, so ask again; the cleanup
        // below ends it when the connection closes or the user changes.
        const delay = Math.min(
          LOAD_RETRY_BASE_MS * 2 ** failedLoads,
          LOAD_RETRY_MAX_MS,
        );
        failedLoads += 1;
        retryTimer = window.setTimeout(() => void load(), delay);
        return;
      }

      loadedOnceRef.current = true;
      setLoaded(true);
      canSaveRef.current = true;
      warnedUnsavedRef.current = false;

      // A save the lost connection failed is owed as well: its read-back
      // failed with it, and the page still shows that change.
      if (unsentUIRef.current || refusedRef.current.ui) {
        unsentUIRef.current = false;
        const ui = queryClient.getQueryData<UIConfig>(uiKey);
        if (ui) enqueueSave({ ui: buildUIPayload(ui) });
      }
    };

    void load();
    return () => {
      cancelled = true;
      window.clearTimeout(retryTimer);
      canSaveRef.current = false;
    };
  }, [isMuxOpen, queryClient, bridgeKey, uiKey, sessionExpired, enqueueSave]);

  // Another tab of this browser saved UI settings: reload them from the
  // bridge. The reload joins the save queue, so it runs after this tab's own
  // pending saves and the cache ends up matching what the bridge holds.
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key !== UI_CONFIG_SAVED_KEY || event.newValue === null) return;
      saveTailRef.current = saveTailRef.current.then(async () => {
        if (!activeQueueRef.current || !canSaveRef.current) return;
        try {
          await queryClient.query({
            ...linuxio.config.get_ui,
            queryKey: uiKey,
            staleTime: CACHE_TTL_MS.NONE,
          });
        } catch (error) {
          const code = error instanceof LinuxIOError ? error.code : 500;
          if (code === 401 || code === 403) sessionExpired();
          // Otherwise this tab keeps the settings it already shows; a change
          // made here mid-reload cancels the reload on purpose.
        }
      });
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, [queryClient, uiKey, sessionExpired]);

  const save = useCallback(
    (patch: ConfigPatch, nextUI: UIConfig, onSaved?: () => void) => {
      const wantsBridge = hasImportantPatch(patch);
      const wantsUI = hasUIPatch(patch);
      if (!wantsBridge && !wantsUI) return;

      if (!canSaveRef.current) {
        // sendablePart left only appearance settings in the patch; the cache
        // keeps them across the reconnect and the next load sends them.
        unsentUIRef.current = true;
        if (!warnedUnsavedRef.current) {
          warnedUnsavedRef.current = true;
          toast.warning(APPEARANCE_SAVED_LATER);
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
      if (nextUI !== ui) {
        // A reload answering after this change would overwrite it.
        void queryClient.cancelQueries({ exact: true, queryKey: uiKey });
        queryClient.setQueryData(uiKey, nextUI);
      }

      if (hasImportantPatch(patch)) {
        // A read-back answering after this change would overwrite it.
        void queryClient.cancelQueries({ exact: true, queryKey: bridgeKey });
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

      const patch = sendablePart(
        { appSettings: { [key]: nextValue } },
        canSaveRef.current,
      );
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
      const resolvedPatch = sendablePart(
        typeof patch === "function"
          ? patch(composeEffectiveConfig(bridge, ui))
          : patch,
        canSaveRef.current,
      );
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
