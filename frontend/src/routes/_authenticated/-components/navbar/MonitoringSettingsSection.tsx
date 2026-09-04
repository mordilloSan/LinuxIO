import { Icon } from "@iconify/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Fragment } from "react";
import { toast } from "sonner";

import {
  CACHE_TTL_MS,
  linuxio,
  type MonitoringConfig,
  type MonitoringConfigPatch,
  type MonitoringListener,
  useCallMutation,
} from "@/api";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useCapability } from "@/hooks/useCapabilities";
import {
  compactGoDuration,
  goDurationToMs,
  isGoDuration,
} from "@/utils/durations";
import { formatFileSize } from "@/utils/formaters";

import {
  SettingsGrid,
  SettingsSaveFooter,
  SettingsSectionShell,
  useSettingsDraft,
} from "./SettingsSectionForm";
import { SectionCard, StatusMetric } from "./SettingsSectionPrimitives";

/**
 * The daemon's plugin registry, in registry order. A listener with an empty
 * selection serves every plugin; anything the daemon reports as a history
 * plugin but that is missing here is appended to the options at render time.
 */
const MONITORING_PLUGINS = [
  "cpu",
  "mem",
  "swap",
  "load",
  "diskio",
  "fs",
  "network",
  "gpu",
  "sensors",
  "containers",
  "container_telemetry",
  "processes",
  "programs",
  "connections",
  "irq",
  "smart",
];

/** Plugins the collector can persist; processes and programs are live-only. */
const HISTORY_PLUGINS = MONITORING_PLUGINS.filter(
  (plugin) => plugin !== "processes" && plugin !== "programs",
);

/** SMART history is written on the SMART refresh, not the collector tick. */
const HISTORY_PLUGINS_WITHOUT_INTERVAL = new Set(["smart"]);

const DISK_USAGE_CACHE_HELPER =
  "How often filesystem usage is re-read; 0 re-reads every time, a value like 15m keeps sleeping disks asleep.";

interface DraftConfig {
  collector_interval: string;
  smart_refresh_interval: string;
  disk_usage_cache: string;
  history_plugins: string[];
  history_intervals: Record<string, string>;
  history_retention: string;
  listeners: MonitoringListener[];
}

interface DraftErrors {
  collector_interval?: string;
  smart_refresh_interval?: string;
  disk_usage_cache?: string;
  history_retention?: string;
  history_intervals?: Partial<Record<string, string>>;
  listener_names?: Partial<Record<number, string>>;
  listener_addresses?: Partial<Record<number, string>>;
}

const toDraft = (config: MonitoringConfig): DraftConfig => ({
  collector_interval: compactGoDuration(config.collector_interval),
  smart_refresh_interval: compactGoDuration(config.smart_refresh_interval),
  disk_usage_cache: compactGoDuration(config.disk_usage_cache),
  history_plugins: splitHistory(config.history),
  history_intervals: Object.fromEntries(
    Object.entries(config.history_intervals ?? {}).map(([plugin, value]) => [
      plugin,
      compactGoDuration(value),
    ]),
  ),
  history_retention: compactGoDuration(config.history_retention),
  listeners: (config.listeners ?? []).map((listener) => ({
    ...listener,
    plugins: [...(listener.plugins ?? [])],
  })),
});

const splitHistory = (history: string) =>
  history
    .split(",")
    .map((plugin) => plugin.trim())
    .filter(Boolean);

/** Registry order first so toggling a plugin yields a stable list; unknown plugins the daemon reported keep their place at the end. */
const joinHistory = (plugins: string[]) => {
  const enabled = new Set(plugins);
  return [
    ...HISTORY_PLUGINS.filter((plugin) => enabled.has(plugin)),
    ...plugins.filter((plugin) => !HISTORY_PLUGINS.includes(plugin)),
  ].join(",");
};

const normalizeIntervals = (intervals: Record<string, string>) =>
  Object.fromEntries(
    Object.entries(intervals)
      .map(([plugin, value]) => [plugin, value.trim()])
      .filter(([, value]) => value !== ""),
  );

const normalizeListeners = (listeners: MonitoringListener[]) =>
  listeners.map((listener) => ({
    ...listener,
    address: listener.address.trim(),
    name: listener.name.trim(),
    plugins: (listener.plugins ?? [])
      .map((plugin) => plugin.trim())
      .filter(Boolean),
  }));

const toPatchPayload = (
  draft: DraftConfig,
  saved: DraftConfig,
): MonitoringConfigPatch => {
  const payload: MonitoringConfigPatch = {};

  if (draft.collector_interval !== saved.collector_interval) {
    payload.collector_interval = draft.collector_interval.trim();
  }
  if (draft.smart_refresh_interval !== saved.smart_refresh_interval) {
    payload.smart_refresh_interval = draft.smart_refresh_interval.trim();
  }
  if (draft.disk_usage_cache !== saved.disk_usage_cache) {
    payload.disk_usage_cache = draft.disk_usage_cache.trim();
  }
  if (
    joinHistory(draft.history_plugins) !== joinHistory(saved.history_plugins)
  ) {
    payload.history = joinHistory(draft.history_plugins);
  }
  const intervals = normalizeIntervals(draft.history_intervals);
  if (
    JSON.stringify(intervals) !==
    JSON.stringify(normalizeIntervals(saved.history_intervals))
  ) {
    payload.history_intervals = intervals;
  }
  if (JSON.stringify(draft.listeners) !== JSON.stringify(saved.listeners)) {
    payload.listeners = normalizeListeners(draft.listeners);
  }

  if (draft.history_retention !== saved.history_retention) {
    payload.history_retention = draft.history_retention.trim();
  }

  return payload;
};

const isListenerAddress = (value: string) => {
  const address = value.trim();
  if (!address) return false;
  if (["none", "off", "disabled"].includes(address.toLowerCase())) {
    return true;
  }
  if (address.startsWith("unix:/")) return true;
  if (address.startsWith("/")) return true;
  if (/^\d+$/.test(address)) return true;
  return address.includes(":");
};

const validateDraft = (draft: DraftConfig): DraftErrors => {
  const errors: DraftErrors = {};

  const interval = draft.collector_interval.trim();
  if (!interval) {
    errors.collector_interval = "Collector interval is required.";
  } else if (!isGoDuration(interval) || interval === "0") {
    errors.collector_interval = "Use a duration like 15s, 1m, or 5m.";
  }

  const smartRefreshInterval = draft.smart_refresh_interval.trim();
  if (!smartRefreshInterval) {
    errors.smart_refresh_interval = "SMART refresh interval is required.";
  } else if (
    !isGoDuration(smartRefreshInterval) ||
    smartRefreshInterval === "0"
  ) {
    errors.smart_refresh_interval = "Use a duration like 1h, 30m, or 12h.";
  }

  // Zero is a valid setting here: it disables the cache and re-reads usage on
  // every collection, so only the duration syntax is checked.
  const diskUsageCache = draft.disk_usage_cache.trim();
  if (!diskUsageCache) {
    errors.disk_usage_cache = "Disk usage cache is required.";
  } else if (!isGoDuration(diskUsageCache)) {
    errors.disk_usage_cache = "Use a duration like 0, 5m, or 15m.";
  }

  const historyRetention = draft.history_retention.trim();
  if (!historyRetention) {
    errors.history_retention = "History retention is required.";
  } else if (!isGoDuration(historyRetention) || historyRetention === "0") {
    errors.history_retention = "Use a duration like 336h or 720h.";
  }

  const tickMs = errors.collector_interval ? null : goDurationToMs(interval);
  const intervalErrors: Partial<Record<string, string>> = {};
  for (const [plugin, value] of Object.entries(draft.history_intervals)) {
    const trimmed = value.trim();
    if (!trimmed) continue;
    const ms = goDurationToMs(trimmed);
    if (ms === null) {
      intervalErrors[plugin] = "Use a duration like 5m or 1h.";
    } else if (tickMs !== null && (ms < tickMs || ms % tickMs !== 0)) {
      intervalErrors[plugin] =
        `Use a whole multiple of the collector interval (${compactGoDuration(interval)}).`;
    }
  }
  if (Object.keys(intervalErrors).length > 0) {
    errors.history_intervals = intervalErrors;
  }

  const nameErrors: Partial<Record<number, string>> = {};
  const addressErrors: Partial<Record<number, string>> = {};
  draft.listeners.forEach((listener, index) => {
    if (!listener.name.trim()) {
      nameErrors[index] = "Required.";
    }
    if (!listener.address.trim()) {
      addressErrors[index] = "Required.";
    } else if (!isListenerAddress(listener.address)) {
      addressErrors[index] =
        "Use host:port, :port, port, unix:/path, or /path.";
    }
  });
  if (Object.keys(nameErrors).length > 0) {
    errors.listener_names = nameErrors;
  }
  if (Object.keys(addressErrors).length > 0) {
    errors.listener_addresses = addressErrors;
  }

  return errors;
};

const hasErrors = (errors: DraftErrors) =>
  Boolean(errors.collector_interval) ||
  Boolean(errors.smart_refresh_interval) ||
  Boolean(errors.disk_usage_cache) ||
  Boolean(errors.history_retention) ||
  Object.values(errors.history_intervals ?? {}).some(Boolean) ||
  Object.values(errors.listener_names ?? {}).some(Boolean) ||
  Object.values(errors.listener_addresses ?? {}).some(Boolean);

const mergeMonitoringDraft = (
  saved: DraftConfig,
  patch: Partial<DraftConfig>,
): DraftConfig => ({
  ...saved,
  ...patch,
});

const getConfigSchemaError = (config: MonitoringConfig): string | null => {
  const data = config as unknown as Record<string, unknown>;
  const requiredDurations = [
    "collector_interval",
    "smart_refresh_interval",
    "history_retention",
  ];
  for (const key of requiredDurations) {
    const value = data[key];
    if (typeof value !== "string") {
      return `monitoring.get_config is missing required string field "${key}".`;
    }
    const trimmed = value.trim();
    if (!trimmed) {
      return `monitoring.get_config field "${key}" must not be empty.`;
    }
    if (!isGoDuration(trimmed) || trimmed === "0") {
      return `monitoring.get_config field "${key}" must be a positive Go duration string.`;
    }
  }

  // Zero disables the disk usage cache, so this one only has to parse.
  if (typeof data.disk_usage_cache !== "string") {
    return `monitoring.get_config is missing required string field "disk_usage_cache".`;
  }
  if (!isGoDuration(data.disk_usage_cache.trim())) {
    return `monitoring.get_config field "disk_usage_cache" must be a Go duration string.`;
  }

  if (typeof data.history !== "string") {
    return `monitoring.get_config is missing required string field "history".`;
  }
  if (
    typeof data.history_intervals !== "object" ||
    data.history_intervals === null ||
    Array.isArray(data.history_intervals)
  ) {
    return `monitoring.get_config is missing required object field "history_intervals".`;
  }

  if (!Array.isArray(data.listeners)) {
    return `monitoring.get_config is missing required array field "listeners".`;
  }
  return null;
};

const MonitoringSettingsSection = () => {
  const queryClient = useQueryClient();
  const {
    isEnabled: monitoringEnabled,
    status: monitoringStatus,
    reason: monitoringReason,
  } = useCapability("monitoringAvailable");

  const {
    data: config,
    isLoading,
    error,
    refetch,
    isFetching,
  } = useQuery({
    ...linuxio.monitoring.get_config,
    enabled: monitoringEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });
  const {
    data: agentStatus,
    error: statusError,
    refetch: refetchStatus,
    isFetching: isStatusFetching,
  } = useQuery({
    ...linuxio.monitoring.get_status,
    enabled: monitoringEnabled,
    staleTime: CACHE_TTL_MS.FIVE_SECONDS,
  });

  const configSchemaError = config ? getConfigSchemaError(config) : null;
  const savedDraft = config && !configSchemaError ? toDraft(config) : null;
  const {
    draft,
    setDraftPatch,
    errors,
    setErrors,
    restartRequired,
    setRestartRequired,
    isDirty,
    patchKey,
    reset: handleReset,
  } = useSettingsDraft<DraftConfig, DraftErrors>(
    savedDraft,
    mergeMonitoringDraft,
  );

  const setConfigMutation = useCallMutation(linuxio.monitoring.set_config, {
    success: (result) => {
      queryClient.setQueryData(
        linuxio.monitoring.get_config.queryKey,
        result.config,
      );
      setDraftPatch({});
      setErrors({});
      setRestartRequired(result.restart_required);
      toast.success("Monitoring settings saved");
      if (result.restart_required) {
        toast.info("Restart linuxio-monitoring to apply listener changes.");
      }
      void refetchStatus();
    },
    error: "Failed to save monitoring settings",
  });
  const restartMutation = useCallMutation(linuxio.monitoring.restart, {
    success: () => {
      setRestartRequired(false);
      toast.success("linuxio-monitoring restarted");
      void refetch();
      void refetchStatus();
    },
    error: "Failed to restart linuxio-monitoring",
  });

  const busy = setConfigMutation.isPending || restartMutation.isPending;
  const refreshing = isFetching || isStatusFetching;

  const updateDraft = <K extends keyof DraftConfig>(
    key: K,
    value: DraftConfig[K],
  ) => {
    patchKey(key, value);
    if (
      key === "collector_interval" ||
      key === "smart_refresh_interval" ||
      key === "disk_usage_cache"
    ) {
      setErrors((prev) => ({ ...prev, [key]: undefined }));
    }
  };

  const toggleHistoryPlugin = (plugin: string, enabled: boolean) => {
    if (!draft) return;
    const plugins = draft.history_plugins.filter((item) => item !== plugin);
    if (enabled) plugins.push(plugin);
    patchKey("history_plugins", plugins);
  };

  const updateHistoryInterval = (plugin: string, value: string) => {
    if (!draft) return;
    patchKey("history_intervals", {
      ...draft.history_intervals,
      [plugin]: value,
    });
    setErrors((prev) => ({
      ...prev,
      history_intervals: { ...prev.history_intervals, [plugin]: undefined },
    }));
  };

  const setListeners = (
    next: (listeners: MonitoringListener[]) => MonitoringListener[],
  ) => {
    setDraftPatch((prev) => {
      if (!savedDraft) return prev;
      const nextListeners = next(prev.listeners ?? savedDraft.listeners);
      if (
        JSON.stringify(nextListeners) === JSON.stringify(savedDraft.listeners)
      ) {
        const cleared = { ...prev };
        delete cleared.listeners;
        return cleared;
      }
      return { ...prev, listeners: nextListeners };
    });
    setRestartRequired(false);
  };

  const updateListener = (
    index: number,
    patch: Partial<MonitoringListener>,
  ) => {
    setListeners((listeners) =>
      listeners.map((listener, listenerIndex) =>
        listenerIndex === index ? { ...listener, ...patch } : listener,
      ),
    );
    setErrors((prev) => ({
      ...prev,
      listener_names: { ...prev.listener_names, [index]: undefined },
      listener_addresses: { ...prev.listener_addresses, [index]: undefined },
    }));
  };

  const addListener = () => {
    setListeners((listeners) => [
      ...listeners,
      { address: "", name: "", plugins: [] },
    ]);
  };

  // Row errors are keyed by index, so a removal invalidates all of them.
  const removeListener = (index: number) => {
    setListeners((listeners) =>
      listeners.filter((_, listenerIndex) => listenerIndex !== index),
    );
    setErrors((prev) => ({
      ...prev,
      listener_addresses: undefined,
      listener_names: undefined,
    }));
  };

  // Success/error handling lives in the setConfigMutation ActionConfig.
  const handleSave = () => {
    if (!draft || !savedDraft) return;
    const nextErrors = validateDraft(draft);
    if (hasErrors(nextErrors)) {
      setErrors(nextErrors);
      return;
    }

    const payload = toPatchPayload(draft, savedDraft);
    if (Object.keys(payload).length === 0) return;

    setConfigMutation.mutate(payload);
  };

  const handleRefresh = () => {
    void refetch();
    void refetchStatus();
  };

  const handleRestart = () => {
    restartMutation.mutate();
  };

  const renderAgentStatusCard = () => (
    <SectionCard
      icon="mdi:chart-line"
      indicator={
        <StatusDot
          absolute
          color="var(--app-palette-success-main)"
          style={{ top: 16, right: 12 }}
          tooltip="Daemon healthy"
        />
      }
      title="Daemon Status"
      titleAdornment={
        agentStatus ? (
          <AppTypography
            color="text.secondary"
            component="span"
            variant="caption"
          >
            ({agentStatus.version})
          </AppTypography>
        ) : undefined
      }
    >
      {statusError ? (
        <AppAlert severity="warning">
          <AppAlertTitle>Status unavailable</AppAlertTitle>
          {statusError.message}
        </AppAlert>
      ) : agentStatus ? (
        <>
          <SettingsGrid minColumnWidth={240} rowGap="var(--app-space-4)">
            <>
              <StatusMetric
                detail={agentStatus.db_path}
                label="Database"
                monoDetail
                value={formatFileSize(agentStatus.db_size_bytes, 0)}
              />
              <StatusMetric
                label={
                  <span
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      gap: "var(--app-space-2)",
                    }}
                  >
                    Config
                    <StatusDot
                      color={
                        agentStatus.config.source === "loaded"
                          ? "var(--app-palette-success-main)"
                          : "var(--app-palette-warning-main)"
                      }
                      size={7}
                      tooltip={`Config ${agentStatus.config.source}`}
                    />
                  </span>
                }
                value={agentStatus.config.path}
              />
            </>
          </SettingsGrid>
          {agentStatus.listeners?.length ? (
            <div style={{ marginTop: "var(--app-space-6)" }}>
              <SettingsGrid minColumnWidth={240} rowGap="var(--app-space-4)">
                {agentStatus.listeners.map((listener) => (
                  <StatusMetric
                    key={listener.name}
                    label={`Listener: ${listener.name}`}
                    value={listener.effective_address || listener.address}
                  />
                ))}
              </SettingsGrid>
            </div>
          ) : null}
        </>
      ) : (
        <div style={{ padding: "var(--app-space-4)" }}>
          <ComponentLoader />
        </div>
      )}
    </SectionCard>
  );

  const shellProps = {
    title: "Monitoring",
    subtitle: "Host metrics daemon (linuxio-monitoring).",
    refreshAriaLabel: "Refresh monitoring settings",
    refreshing,
    refreshDisabled: !monitoringEnabled,
    onRefresh: handleRefresh,
  };

  if (!monitoringEnabled) {
    return (
      <SettingsSectionShell {...shellProps}>
        <AppAlert
          severity={monitoringStatus === "unknown" ? "info" : "warning"}
        >
          <AppAlertTitle>
            {monitoringStatus === "unknown"
              ? "Checking Monitoring"
              : "Monitoring unavailable"}
          </AppAlertTitle>
          {monitoringReason}
        </AppAlert>
      </SettingsSectionShell>
    );
  }

  if (error) {
    return (
      <SettingsSectionShell {...shellProps}>
        <AppAlert severity="error">
          <AppAlertTitle>Monitoring settings unavailable</AppAlertTitle>
          {error.message}
        </AppAlert>
      </SettingsSectionShell>
    );
  }

  if (configSchemaError) {
    return (
      <SettingsSectionShell {...shellProps}>
        {renderAgentStatusCard()}
        <AppAlert severity="error">
          <AppAlertTitle>Monitoring config contract mismatch</AppAlertTitle>
          {configSchemaError} Update linuxio-monitoring so its config API
          matches the LinuxIO monitoring settings contract.
        </AppAlert>
      </SettingsSectionShell>
    );
  }

  if (isLoading || !draft) {
    return (
      <SettingsSectionShell {...shellProps}>
        <div style={{ padding: "var(--app-space-12)" }}>
          <ComponentLoader />
        </div>
      </SettingsSectionShell>
    );
  }

  // The two fixed sockets (api, control) are not configurable and never appear
  // in config.listeners, so every draft listener is editable.
  const pluginOptions = [
    ...MONITORING_PLUGINS,
    ...(agentStatus?.config.history_plugins ?? []).filter(
      (plugin) => !MONITORING_PLUGINS.includes(plugin),
    ),
  ];

  return (
    <SettingsSectionShell {...shellProps}>
      {restartRequired ? (
        <AppAlert severity="info">
          <AppAlertTitle>Restart required</AppAlertTitle>
          Some saved settings need linuxio-monitoring to restart before they
          fully apply.
          <div style={{ marginTop: "var(--app-space-4)" }}>
            <AppButton
              disabled={busy}
              onClick={handleRestart}
              size="small"
              startIcon={
                <Icon
                  height={16}
                  icon={
                    restartMutation.isPending ? "mdi:loading" : "mdi:restart"
                  }
                  width={16}
                />
              }
              variant="outlined"
            >
              {restartMutation.isPending ? "Restarting..." : "Restart agent"}
            </AppButton>
          </div>
        </AppAlert>
      ) : null}

      {renderAgentStatusCard()}

      <SectionCard
        icon="mdi:connection"
        subtitle="Read-only metrics API bind addresses"
        title="Listeners"
      >
        <AppAlert severity="info">
          Listeners are unauthenticated. Anyone who can reach the address can
          read the selected metrics.
        </AppAlert>
        {draft.listeners.length === 0 ? (
          <AppTypography
            color="text.secondary"
            style={{ display: "block", marginTop: "var(--app-space-6)" }}
            variant="caption"
          >
            No listeners configured. Add one to expose read-only metrics on a
            TCP address.
          </AppTypography>
        ) : (
          draft.listeners.map((listener, index) => (
            <div
              key={index}
              style={{ marginTop: "var(--app-space-6)", minWidth: 0 }}
            >
              <SettingsGrid>
                <>
                  <AppTextField
                    disabled={busy}
                    error={Boolean(errors.listener_names?.[index])}
                    fullWidth
                    helperText={errors.listener_names?.[index]}
                    label="Name"
                    onChange={(event) =>
                      updateListener(index, { name: event.target.value })
                    }
                    placeholder="lan"
                    shrinkLabel
                    size="small"
                    value={listener.name}
                  />
                  <AppTextField
                    disabled={busy}
                    error={Boolean(errors.listener_addresses?.[index])}
                    fullWidth
                    helperText={errors.listener_addresses?.[index]}
                    label="Address"
                    onChange={(event) =>
                      updateListener(index, { address: event.target.value })
                    }
                    placeholder="0.0.0.0:45876"
                    shrinkLabel
                    size="small"
                    value={listener.address}
                  />
                  <AppAutocomplete
                    disabled={busy}
                    fullWidth
                    helperText="Empty serves every plugin."
                    label="Plugins"
                    multiple
                    onChange={(plugins) => updateListener(index, { plugins })}
                    options={pluginOptions}
                    shrinkLabel
                    size="small"
                    value={listener.plugins ?? []}
                  />
                </>
              </SettingsGrid>
              <div
                style={{
                  display: "flex",
                  justifyContent: "flex-end",
                  marginTop: "var(--app-space-2)",
                }}
              >
                <AppButton
                  aria-label="Remove listener"
                  disabled={busy}
                  onClick={() => removeListener(index)}
                  size="small"
                  startIcon={
                    <Icon height={16} icon="mdi:delete-outline" width={16} />
                  }
                  variant="outlined"
                >
                  Remove
                </AppButton>
              </div>
            </div>
          ))
        )}
        <div style={{ display: "flex", marginTop: "var(--app-space-6)" }}>
          <AppButton
            disabled={busy}
            onClick={addListener}
            size="small"
            startIcon={<Icon height={16} icon="mdi:plus" width={16} />}
            variant="outlined"
          >
            Add listener
          </AppButton>
        </div>
      </SectionCard>

      <SectionCard
        icon="mdi:timer-cog-outline"
        subtitle="Sampling cadence and history retention plugins"
        title="Collector"
      >
        <SettingsGrid>
          <>
            <AppTooltip title="How often metrics are sampled">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.collector_interval)}
                fullWidth
                helperText={errors.collector_interval}
                label="Collector interval"
                onChange={(event) =>
                  updateDraft("collector_interval", event.target.value)
                }
                size="small"
                value={draft.collector_interval}
              />
            </AppTooltip>
            <AppTooltip title="How often SMART data is refreshed">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.smart_refresh_interval)}
                fullWidth
                helperText={errors.smart_refresh_interval}
                label="SMART refresh"
                onChange={(event) =>
                  updateDraft("smart_refresh_interval", event.target.value)
                }
                size="small"
                value={draft.smart_refresh_interval}
              />
            </AppTooltip>
            <AppTextField
              disabled={busy}
              error={Boolean(errors.disk_usage_cache)}
              fullWidth
              helperText={errors.disk_usage_cache ?? DISK_USAGE_CACHE_HELPER}
              label="Disk usage cache"
              onChange={(event) =>
                updateDraft("disk_usage_cache", event.target.value)
              }
              size="small"
              value={draft.disk_usage_cache}
            />
            <AppTooltip title="How long one-minute history is retained">
              <AppTextField
                disabled={busy}
                error={Boolean(errors.history_retention)}
                fullWidth
                helperText={errors.history_retention}
                label="History retention"
                onChange={(event) =>
                  updateDraft("history_retention", event.target.value)
                }
                size="small"
                value={draft.history_retention}
              />
            </AppTooltip>
          </>
        </SettingsGrid>

        <AppTypography
          color="text.secondary"
          style={{ display: "block", marginTop: "var(--app-space-6)" }}
          variant="caption"
        >
          History plugins. Each interval is a whole multiple of the collector
          interval; empty means every tick.
        </AppTypography>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "minmax(160px, 1fr) minmax(120px, 180px)",
            columnGap: "var(--app-space-6)",
            rowGap: "var(--app-space-2)",
            alignItems: "center",
            marginTop: "var(--app-space-2)",
          }}
        >
          {HISTORY_PLUGINS.map((plugin) => {
            const enabled = draft.history_plugins.includes(plugin);
            return (
              <Fragment key={plugin}>
                <AppFormControlLabel
                  control={
                    <AppCheckbox
                      checked={enabled}
                      onChange={(event) =>
                        toggleHistoryPlugin(plugin, event.target.checked)
                      }
                      size="small"
                    />
                  }
                  disabled={busy}
                  label={plugin}
                />
                {HISTORY_PLUGINS_WITHOUT_INTERVAL.has(plugin) ? (
                  <AppTypography color="text.secondary" variant="caption">
                    Follows SMART refresh
                  </AppTypography>
                ) : (
                  <AppTextField
                    aria-label={`${plugin} interval`}
                    disabled={busy || !enabled}
                    error={Boolean(errors.history_intervals?.[plugin])}
                    fullWidth
                    helperText={errors.history_intervals?.[plugin]}
                    onChange={(event) =>
                      updateHistoryInterval(plugin, event.target.value)
                    }
                    placeholder={draft.collector_interval}
                    size="small"
                    value={draft.history_intervals[plugin] ?? ""}
                  />
                )}
              </Fragment>
            );
          })}
        </div>
      </SectionCard>

      <SettingsSaveFooter
        busy={busy}
        isDirty={isDirty}
        onReset={handleReset}
        onSave={handleSave}
        saveDisabled={hasErrors(errors)}
        saving={setConfigMutation.isPending}
      />
    </SettingsSectionShell>
  );
};

export default MonitoringSettingsSection;
